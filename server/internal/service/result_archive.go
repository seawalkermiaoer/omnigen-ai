// Package service: result_archive.go 把上游返回的生成结果转存到我们自己的
// OSS，并返回替换后的永久公开 URL。
//
// 存在的理由：generation_tasks.result_urls 里原本存的是上游（t8star /
// DashScope）返回的链接，这类链接是短期有效的——也就是说「历史记录」这个
// 功能过一段时间就整体烂掉，而它的全部价值就建立在结果还能打开上。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/mimeext"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/urlallow"
)

// ResultArchiver 把一组上游结果 URL 转存到 OSS。
//
// Archive 不返回 error，这是刻意的设计，不要「顺手改好」：调用它的时刻，
// 上游已经真的出了图、token 已经真的扣了、用户配额也已经真的扣了。如果
// OSS 没配置、网络抖动、或对象存储拒绝写入，正确行为是保留原始上游 URL 并
// 记日志，而不是把一次已经付过钱的成功生成翻译成失败——那样用户既丢钱又
// 丢结果，还会重试再付一次。
//
// 降级是逐条的：三条结果里第二条失败，第一、三条仍用 OSS URL。返回切片的
// 长度恒等于入参长度，顺序一一对应，调用方可以直接拿去写库。
type ResultArchiver interface {
	Archive(ctx context.Context, taskID int64, urls []string) []string
}

const (
	// maxArchiveBytes 是单个结果的体积上限。超过即放弃归档、退回上游 URL：
	// 归档是事后增强，不值得为一个异常大的响应把内存和带宽拖垮。
	maxArchiveBytes = 200 << 20 // 200MB

	// archiveTimeout 比 download.go 的 60s 宽松，因为这里要完整下载 + 完整
	// 上传一段视频（几十 MB 走两趟网络），60s 会把正常的大视频误判成失败。
	archiveTimeout = 10 * time.Minute
)

var (
	errArchiveTooLarge        = errors.New("上游结果超过归档体积上限")
	errTooManyRedirects       = errors.New("重定向跳数超过上限")
	errRedirectHostNotAllowed = errors.New("重定向目标主机不在白名单内")
)

// ResultArchiveService 是生产实现。它复用 settingsOSSResolver（upload_oss.go）
// 解析 OSS 配置——那份逻辑已经处理了 direct AK/SK 与 STS 两种模式以及
// app_settings 热更新，再写一份只会分叉。
type ResultArchiveService struct {
	resolver    OSSResolver
	client      *http.Client
	hostAllowed func(host string) bool
	maxBytes    int64
}

// NewResultArchiver 构造生产归档服务：OSS 配置在每次归档时从 app_settings
// 现读，管理员改配置无需重启。
func NewResultArchiver(settings SettingReader) *ResultArchiveService {
	return newResultArchiverWithDeps(newSettingsOSSResolver(settings), urlallow.IsAllowedResultHost, maxArchiveBytes)
}

// newResultArchiverWithDeps 是测试注入点：替换 OSSResolver（避免碰真实
// OSS）、替换 host 过滤器（让 httptest 的回环地址能顶替真实结果域名，同时
// 仍然走同一段强制逻辑）、以及压低体积上限（测试造不出 200MB 响应）。
func newResultArchiverWithDeps(resolver OSSResolver, hostAllowed func(string) bool, maxBytes int64) *ResultArchiveService {
	a := &ResultArchiveService{resolver: resolver, hostAllowed: hostAllowed, maxBytes: maxBytes}
	a.client = &http.Client{
		Timeout: archiveTimeout,
		// 与 download.go 同样的执行路径：白名单在初始 URL 和每一跳重定向上
		// 都要生效。只校验初始 URL 是这类防护最经典的绕过方式。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > urlallow.MaxRedirects {
				return errTooManyRedirects
			}
			if !a.hostAllowed(req.URL.Hostname()) {
				return errRedirectHostNotAllowed
			}
			return nil
		},
	}
	return a
}

// Archive 实现 ResultArchiver。
func (a *ResultArchiveService) Archive(ctx context.Context, taskID int64, urls []string) []string {
	if len(urls) == 0 {
		return urls
	}

	// 先把入参原样拷一份作为兜底：后面每成功一条才覆盖一条，任何提前退出
	// 的分支返回的都是完整的上游 URL 列表，长度和顺序天然对得上。
	out := make([]string, len(urls))
	copy(out, urls)

	store, err := a.resolver.Resolve(ctx)
	if err != nil {
		// OSS 未配置是最常见的分支（全新部署、管理员还没填），它不是错误路径，
		// 是降级路径：生成照样成功，只是历史记录里存的仍是会过期的上游链接。
		slog.Warn("结果归档跳过：OSS 不可用，保留上游 URL", "taskID", taskID, "err", err)
		return out
	}

	for i, raw := range urls {
		archived, err := a.archiveOne(ctx, store, taskID, i, raw)
		if err != nil {
			slog.Warn("结果归档失败，该条保留上游 URL",
				"taskID", taskID, "index", i, "url", raw, "err", err)
			continue
		}
		out[i] = archived
	}
	return out
}

// archiveOne 抓取单条上游结果并流式上传到 OSS，返回永久公开 URL。
func (a *ResultArchiveService) archiveOne(ctx context.Context, store ossx.Store, taskID int64, index int, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("结果 URL 不合法: %q", raw)
	}
	// 这是一条新的「服务端按 URL 取内容」路径，也就是一个新的 SSRF 面。
	// 用的是 download.go 同一份白名单（urlallow），不另开口子。
	if !a.hostAllowed(parsed.Hostname()) {
		return "", fmt.Errorf("结果主机不在白名单内: %q", parsed.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "omnigen-web/1.0")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("上游返回 HTTP %d", resp.StatusCode)
	}

	// 上游声明了长度且已经超限时，连body 都不用读——省掉一次注定要丢弃的
	// 完整下载。没声明长度（分块传输）时靠下面的 cappedReader 兜底。
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n > a.maxBytes {
			return "", fmt.Errorf("%w: Content-Length=%d", errArchiveTooLarge, n)
		}
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	key, err := archiveObjectKey(contentType)
	if err != nil {
		return "", err
	}

	// resp.Body 直接透传给 PutPublic：视频可能几十 MB，整体读进内存意味着
	// 并发归档时每个任务都在堆上压一份完整文件。cappedReader 在读取过程中
	// 封顶，超限时上传会带着错误中断（对象不会被建立），走上层降级分支。
	return store.PutPublic(ctx, key, &cappedReader{r: resp.Body, limit: a.maxBytes}, contentType)
}

// archiveObjectKey 生成 results/{YYYY}/{MM}/{DD}/{8位随机}{ext}。
//
// **按日期分区，不带 taskID。** 这一版曾经是 results/{taskID}/{index}-...，
// 但那个形状和「归档必须发生在落库之前」这条要求直接冲突：图片是同步生成的，
// 归档那一刻数据库行还没写、根本没有 ID，只能传 0，于是所有图片结果全堆进
// results/0/ 一个目录，按任务分目录的便利完全落空。
//
// 换成日期分区把冲突消掉，并且顺带更有用：OSS 的生命周期规则是按前缀匹配的，
// 「删除 N 天前的结果」直接就能落到这个键上；而 taskID→URL 的映射本来就完整
// 记在 generation_tasks.result_urls 里，不需要靠对象键再存一份。
//
// 随机段不是装饰：对象是 public-read 的，可预测的键意味着任何人都能遍历出
// 别人的生成结果。它同时也是同一天内的唯一性来源——键里已经没有 taskID 和
// index 了，去掉随机段会让同一天的结果互相覆盖。
//
// 扩展名从 Content-Type 推导并经 mimeext.Sanitize 过滤，所以上游 header 里的
// 路径分隔符/换行进不了键。
func archiveObjectKey(contentType string) (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// 随机数取不到时宁可放弃归档也不退化成可枚举的键。
		return "", fmt.Errorf("生成随机对象键失败: %w", err)
	}
	suffix := ""
	if ext := mimeext.FromContentType(contentType); ext != "" {
		suffix = "." + ext
	}
	return fmt.Sprintf("results/%s/%s%s",
		time.Now().UTC().Format("2006/01/02"), hex.EncodeToString(buf[:]), suffix), nil
}

// cappedReader 在累计读取超过 limit 时报错。用它而不是 io.LimitReader 是
// 因为 LimitReader 到达上限只会返回 EOF——那会让一个被截断的半截视频
// 「成功」传上 OSS 并写进数据库，比归档失败更糟。
type cappedReader struct {
	r     io.Reader
	limit int64
	read  int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	if c.read > c.limit {
		return n, errArchiveTooLarge
	}
	return n, err
}

var _ ResultArchiver = (*ResultArchiveService)(nil)
