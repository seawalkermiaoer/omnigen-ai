package service

// 白盒测试（package service）：归档服务的体积上限 maxArchiveBytes 是 200MB，
// 测试里没法真的造一个 200MB 的响应，所以构造函数
// newResultArchiverWithDeps 允许注入一个小得多的上限。这个注入点是未导出的，
// 只有同包测试能用——与 upload_oss_test.go 白盒测试 settingsOSSResolver 同理。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
)

// ── fakes ────────────────────────────────────────────────────────────────

type archivedObject struct {
	key         string
	contentType string
	body        []byte
}

// fakeArchiveStore 记录每一次 PutPublic。failKeys 让测试可以只让某一条上传
// 失败——「逐条降级」这条要求只有在部分失败时才看得出来。
type fakeArchiveStore struct {
	mu      sync.Mutex
	objects []archivedObject
	failOn  func(key string) bool
}

func (f *fakeArchiveStore) PutPublic(_ context.Context, key string, body io.Reader, contentType string) (string, error) {
	// 先读 body：真实 SDK 也是边读边传，读的过程中出错（比如超过体积上限的
	// cappedReader）必须表现为上传失败，而不是悄悄传上去一个截断的对象。
	b, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn != nil && f.failOn(key) {
		return "", errors.New("模拟 OSS 写入失败")
	}
	f.objects = append(f.objects, archivedObject{key: key, contentType: contentType, body: b})
	return "https://test-bucket.oss-cn-chengdu.aliyuncs.com/" + key, nil
}

func (f *fakeArchiveStore) Put(context.Context, string, []byte, string) error {
	return errors.New("unexpected: 归档只应走 PutPublic")
}

func (f *fakeArchiveStore) SignedURL(context.Context, string) (string, error) {
	return "", errors.New("unexpected: 归档不应签名 URL")
}

func (f *fakeArchiveStore) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.objects))
	for _, o := range f.objects {
		out = append(out, o.key)
	}
	return out
}

var _ ossx.Store = (*fakeArchiveStore)(nil)

type fakeArchiveResolver struct {
	store ossx.Store
	err   error
}

func (f *fakeArchiveResolver) Resolve(context.Context) (ossx.Store, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.store, nil
}

// loopbackOnly 站在生产 host 白名单的位置上：httptest.Server 跑在
// 127.0.0.1，真实白名单（urlallow）当然不认它，所以测试注入一个只认回环地址
// 的过滤器——走的仍然是同一段强制逻辑（初始 URL + 每一跳重定向）。
func loopbackOnly(host string) bool { return host == "127.0.0.1" }

func newTestArchiver(store ossx.Store, maxBytes int64) *ResultArchiveService {
	return newResultArchiverWithDeps(&fakeArchiveResolver{store: store}, loopbackOnly, maxBytes)
}

// upstreamServer 提供归档要抓的「上游结果」。路径决定行为，见各用例。
//
// 它还额外起了一个「白名单外」的服务器：同一台机器、同样活着、只是主机名
// 是 localhost 而不是 127.0.0.1，因而不被 loopbackOnly 接受。用一个真实可
// 达的服务器（而不是一个解析不了的假域名）当重定向目标是有意的——假域名会
// 让请求死在 DNS 上，测试就算把 CheckRedirect 整段删掉也照样绿。
func upstreamServer(t *testing.T) *httptest.Server {
	t.Helper()

	offAllowlist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("SECRET-FROM-OFF-ALLOWLIST-HOST"))
	}))
	t.Cleanup(offAllowlist.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(offAllowlist.URL, "http://"))
	require.NoError(t, err)
	offAllowlistURL := "http://localhost:" + port + "/evil.png"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ok"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG-BYTES-" + r.URL.Path))
		case r.URL.Path == "/video":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("MP4-BYTES"))
		case r.URL.Path == "/missing":
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/slow":
			time.Sleep(500 * time.Millisecond)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("late"))
		case r.URL.Path == "/big":
			// 显式声明 Content-Length：net/http 只在响应小到能整块缓冲时才
			// 自动补这个头，不写死的话这条用例会悄悄退化成分块传输，
			// 「读 body 之前就拒掉」那个分支根本走不到。
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "4096")
			_, _ = w.Write(make([]byte, 4096))
		case r.URL.Path == "/big-chunked":
			// 不声明长度（分块传输）：只能靠读取过程中的封顶发现超限。
			w.Header().Set("Content-Type", "image/png")
			flusher, ok := w.(http.Flusher)
			require.True(t, ok)
			for i := 0; i < 8; i++ {
				_, _ = w.Write(make([]byte, 512))
				flusher.Flush()
			}
		case r.URL.Path == "/redirect-evil":
			http.Redirect(w, r, offAllowlistURL, http.StatusFound)
		case r.URL.Path == "/redirect-ok":
			http.Redirect(w, r, "/ok.png", http.StatusFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ── 降级：绝不把已经成功（已扣费）的生成翻译成失败 ────────────────────────

func TestArchive_OSSNotConfigured_ReturnsUpstreamURLsUnchanged(t *testing.T) {
	a := newResultArchiverWithDeps(
		&fakeArchiveResolver{err: errors.New("OSS 未配置")}, loopbackOnly, maxArchiveBytes)

	urls := []string{"https://webstatic.aiproxy.vip/output/a.png", "https://webstatic.aiproxy.vip/output/b.png"}
	got := a.Archive(context.Background(), 1, urls)

	assert.Equal(t, urls, got)
}

func TestArchive_EmptyInput_ReturnsEmpty(t *testing.T) {
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	got := a.Archive(context.Background(), 1, []string{})

	assert.Empty(t, got)
	assert.Empty(t, store.keys(), "空输入不应产生任何上传")
}

func TestArchive_AllSucceed_NoUpstreamURLSurvives(t *testing.T) {
	up := upstreamServer(t)
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	urls := []string{up.URL + "/ok1.png", up.URL + "/ok2.png", up.URL + "/video"}
	got := a.Archive(context.Background(), 7, urls)

	require.Len(t, got, 3)
	for i, u := range got {
		assert.True(t, strings.HasPrefix(u, "https://test-bucket.oss-cn-chengdu.aliyuncs.com/"),
			"第 %d 条应当是 OSS URL，实际 %q", i, u)
		assert.NotContains(t, u, up.URL, "不允许有任何一条上游 URL 残留")
	}
	assert.Len(t, store.keys(), 3)
}

func TestArchive_SecondItemFails_OnlyThatItemFallsBack(t *testing.T) {
	up := upstreamServer(t)
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	// 第二条抓不到（404），另外两条正常。
	urls := []string{up.URL + "/ok1.png", up.URL + "/missing", up.URL + "/ok3.png"}
	got := a.Archive(context.Background(), 7, urls)

	require.Len(t, got, 3, "返回长度必须始终等于入参长度")
	assert.Contains(t, got[0], "aliyuncs.com")
	assert.Equal(t, urls[1], got[1], "失败的那条退回上游 URL，不影响其余两条")
	assert.Contains(t, got[2], "aliyuncs.com")
}

func TestArchive_UploadFails_ThatItemFallsBack(t *testing.T) {
	up := upstreamServer(t)
	// 让第二条（index 1）失败：键里已不含 index，改用调用序号来选。
	var seen int
	store := &fakeArchiveStore{failOn: func(string) bool { seen++; return seen == 2 }}
	a := newTestArchiver(store, maxArchiveBytes)

	urls := []string{up.URL + "/ok1.png", up.URL + "/ok2.png"}
	got := a.Archive(context.Background(), 7, urls)

	require.Len(t, got, 2)
	assert.Contains(t, got[0], "aliyuncs.com")
	assert.Equal(t, urls[1], got[1])
}

func TestArchive_UpstreamTimeout_FallsBack(t *testing.T) {
	up := upstreamServer(t)
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	urls := []string{up.URL + "/slow"}
	got := a.Archive(ctx, 7, urls)

	assert.Equal(t, urls, got)
	assert.Empty(t, store.keys())
}

func TestArchive_BodyExceedsMaxBytes_FallsBack(t *testing.T) {
	up := upstreamServer(t)

	// 两个子用例钉的是同一个可观察结果（超限即退回上游 URL），区别只在上游
	// 有没有声明长度。声明长度时 archiveOne 会在读 body 之前就拒掉——那是省
	// 带宽的优化，行为上与 cappedReader 兜底不可区分，所以这里不去断言「是哪
	// 一道闸拦下的」，只保证两条路径都拦得住。
	t.Run("声明了 Content-Length", func(t *testing.T) {
		store := &fakeArchiveStore{}
		a := newTestArchiver(store, 1024)

		urls := []string{up.URL + "/big"}
		got := a.Archive(context.Background(), 7, urls)

		assert.Equal(t, urls, got)
		assert.Empty(t, store.keys(), "已知超限时不该开始上传")
	})

	t.Run("分块传输、长度未知", func(t *testing.T) {
		store := &fakeArchiveStore{}
		a := newTestArchiver(store, 1024)

		urls := []string{up.URL + "/big-chunked"}
		got := a.Archive(context.Background(), 7, urls)

		assert.Equal(t, urls, got, "读取过程中超限也必须退回上游 URL")
		assert.Empty(t, store.keys())
	})
}

// ── SSRF：host 白名单在初始 URL 和每一跳重定向上都要生效 ──────────────────

func TestArchive_InitialHostNotAllowed_Rejected(t *testing.T) {
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	urls := []string{"http://169.254.169.254/latest/meta-data/"}
	got := a.Archive(context.Background(), 7, urls)

	assert.Equal(t, urls, got)
	assert.Empty(t, store.keys(), "白名单外的 host 根本不该被抓取")
}

func TestArchive_NonHTTPScheme_Rejected(t *testing.T) {
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	urls := []string{"file:///etc/passwd"}
	got := a.Archive(context.Background(), 7, urls)

	assert.Equal(t, urls, got)
	assert.Empty(t, store.keys())
}

func TestArchive_RedirectToDisallowedHost_Rejected(t *testing.T) {
	up := upstreamServer(t)
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	// 初始 URL 在白名单内，只有重定向目标不在——只校验初始 URL 的实现会在
	// 这里放行，这正是这类防护最经典的绕过方式。
	urls := []string{up.URL + "/redirect-evil"}
	got := a.Archive(context.Background(), 7, urls)

	assert.Equal(t, urls, got)
	assert.Empty(t, store.keys())
}

func TestArchive_RedirectWithinAllowlist_Followed(t *testing.T) {
	up := upstreamServer(t)
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	got := a.Archive(context.Background(), 7, []string{up.URL + "/redirect-ok"})

	require.Len(t, got, 1)
	assert.Contains(t, got[0], "aliyuncs.com", "白名单内的重定向应当正常跟随")
}

// ── 对象键 ────────────────────────────────────────────────────────────────

// 键按**日期**分区，不含 taskID —— 图片是同步生成的，归档发生在落库之前，
// 那一刻根本还没有 taskID。原先键里带 taskID 的写法在图片这条路上只能传 0，
// 结果是所有图片结果全堆进 results/0/ 一个目录。日期分区顺带让 OSS 的生命
// 周期规则（按前缀删除 N 天前的对象）能直接落到这个键上。
var archiveKeyPattern = regexp.MustCompile(`^results/\d{4}/\d{2}/\d{2}/[0-9a-f]{8}\.png$`)

func TestArchive_ObjectKeyShapeAndExtension(t *testing.T) {
	up := upstreamServer(t)
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	a.Archive(context.Background(), 7, []string{up.URL + "/ok1.png"})

	keys := store.keys()
	require.Len(t, keys, 1)
	assert.Regexp(t, archiveKeyPattern, keys[0])
	assert.Equal(t, "image/png", store.objects[0].contentType)
}

func TestArchive_VideoExtensionFromContentType(t *testing.T) {
	up := upstreamServer(t)
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	a.Archive(context.Background(), 7, []string{up.URL + "/video"})

	keys := store.keys()
	require.Len(t, keys, 1)
	assert.True(t, strings.HasSuffix(keys[0], ".mp4"), "扩展名应由 Content-Type 推导，实际 %q", keys[0])
}

// TestArchive_KeysAreUnpredictable 是 public-read 的直接后果：键可枚举等于
// 任何人都能遍历出别人的生成结果。同一个 taskID/index 两次归档必须落在不同键上。
func TestArchive_KeysAreUnpredictable(t *testing.T) {
	up := upstreamServer(t)
	store := &fakeArchiveStore{}
	a := newTestArchiver(store, maxArchiveBytes)

	a.Archive(context.Background(), 7, []string{up.URL + "/ok1.png"})
	a.Archive(context.Background(), 7, []string{up.URL + "/ok1.png"})

	keys := store.keys()
	require.Len(t, keys, 2)
	assert.NotEqual(t, keys[0], keys[1], fmt.Sprintf("同一 taskID/index 的两次归档产生了相同的键 %q", keys[0]))
}

var _ ResultArchiver = (*ResultArchiveService)(nil)
