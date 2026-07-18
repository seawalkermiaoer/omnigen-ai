package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/crypto"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// UpstreamCredentials 是探测一组上游凭证有效性所需的最小信息集合，
// 对应 app_settings 里 dashscope_api_key/region/endpoint/workspace_id 四项。
type UpstreamCredentials struct {
	APIKey      string
	Region      string
	Endpoint    string
	WorkspaceID string
}

// ConnectionTester 用给定凭证试调上游，返回 nil 表示凭证有效。
//
// 本任务（Task 4）与 internal/provider/dashscope 客户端（Task 6）是并行开发
// 的两个任务，provider 层此刻还不存在。这里没有等 Task 6 落地，而是自己拥有
// 一个只依赖凭证参数的窄接口：默认实现 defaultConnectionTester 直接打
// DashScope 的 compatible-mode/chat-completions（旧系统里最省钱的探测路
// 径，参见 server.js 的 REGION_ENDPOINTS/getEndpoint），不依赖任何 provider
// 包。等 Task 6 的 dashscope.Client 就绪后，可以把 NewSettingService 默认
// 传入的实现换成真正的 client，这个接口与 SettingService 的其余部分都不用
// 改——service 测试用假 ConnectionTester 验证的是"凭证被正确解密并传递"，
// 与探测请求具体怎么打无关。
type ConnectionTester interface {
	Test(ctx context.Context, creds UpstreamCredentials) error
}

// SettingService 承载配置与密钥的业务规则：加解密、"空值=不修改"语义、
// 脱敏响应、上游连通性探测。
type SettingService struct {
	settings repository.SettingRepository
	tester   ConnectionTester
}

// NewSettingService 构造生产用的 SettingService，TestConnection 走默认的
// DashScope compatible-mode 探测实现。
func NewSettingService(settings repository.SettingRepository) *SettingService {
	return NewSettingServiceWithTester(settings, defaultConnectionTester{})
}

// NewSettingServiceWithTester 允许调用方（生产环境接入真正的 provider 客户端、
// 测试环境注入假实现）指定 ConnectionTester。
func NewSettingServiceWithTester(settings repository.SettingRepository, tester ConnectionTester) *SettingService {
	return &SettingService{settings: settings, tester: tester}
}

// Get 返回全部配置项的脱敏视图，固定按 settingmodel.AllKeys 的顺序、
// 且总是包含全部键——即便某个键在数据库里从未被写过，也以"未配置"的
// 形态出现在响应里，前端渲染表单不需要先判断某一项存不存在。
//
// 每个 secret 项在交给 settingmodel.FromEntity 之前都先解密成明文
// （decryptIfSecret）：FromEntity 假定传入的 Value 已经是明文，如果这里把
// 密文原样传过去，Configured 依然会报 true（密文非空），但 Masked 会脱敏
// 出一坨乱码密文而不是真正密钥的首尾——这是 Task 2 作者留下的提醒，
// 见 TestGet_DecryptsBeforeMapping。
func (s *SettingService) Get(ctx context.Context) (*settingmodel.SettingsResponse, error) {
	rows, err := s.settings.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]settingmodel.Setting, len(rows))
	for _, r := range rows {
		byKey[r.Key] = r
	}

	items := make([]settingmodel.Setting, 0, len(settingmodel.AllKeys))
	for _, k := range settingmodel.AllKeys {
		row, ok := byKey[string(k)]
		if !ok {
			row = settingmodel.Setting{Key: string(k), IsSecret: k.IsSecret()}
		}
		plain, err := decryptIfSecret(row)
		if err != nil {
			return nil, err
		}
		row.Value = plain
		items = append(items, row)
	}
	resp := settingmodel.FromEntities(items)
	return &resp, nil
}

// GetDecrypted 返回单个配置项的明文值，供后续任务（图片/视频生成 service）
// 内部使用——绝不能经由 handler 暴露到 HTTP 响应。键从未配置（数据库里没
// 有这一行）与配置成空字符串被统一当作"未配置"，返回 "", nil：调用方只需
// 判断返回值是否为空，不用再区分 apperr.ErrSettingNotFound 这种存储细节。
func (s *SettingService) GetDecrypted(ctx context.Context, key settingmodel.Key) (string, error) {
	row, err := s.settings.Get(ctx, string(key))
	if err != nil {
		if errors.Is(err, apperr.ErrSettingNotFound) {
			return "", nil
		}
		return "", err
	}
	return decryptIfSecret(*row)
}

// decryptIfSecret 把一条可能是密文的 Setting 转成明文 Value。非 secret 项、
// 或 secret 项但当前是空字符串（未配置/已清空）原样返回，不尝试解密——
// crypto.Decrypt 只认 Encrypt 产出的 "iv:tag:ciphertext" 格式，拿空串去解会
// 直接报格式错误。
func decryptIfSecret(s settingmodel.Setting) (string, error) {
	if !s.IsSecret || s.Value == "" {
		return s.Value, nil
	}
	plain, err := crypto.Decrypt(s.Value, s.Key)
	if err != nil {
		return "", apperr.ErrInternal.Wrap(fmt.Errorf("解密配置项 %s 失败: %w", s.Key, err))
	}
	return plain, nil
}

// isKnownKey 报告 key 是否是 settingmodel.AllKeys 里声明过的合法配置项。
func isKnownKey(key settingmodel.Key) bool {
	for _, k := range settingmodel.AllKeys {
		if k == key {
			return true
		}
	}
	return false
}

// Update 按"空值=不修改，Clear=true 才真正清空"的语义批量写入配置项，
// actorID 必须来自 handler 层的 middleware.UserIDFrom（认证上下文），
// 绝不能取自请求体——与 UserService 的 actorID 契约一致。
//
// 校验先于任何写入：一旦发现未知 key，整个请求不落地任何一项，避免
// "前面几项生效、最后一项报错"这种部分写入的模糊状态。
func (s *SettingService) Update(ctx context.Context, actorID int64, req settingmodel.UpdateRequest) (*settingmodel.SettingsResponse, error) {
	entries := make([]settingmodel.Setting, 0, len(req.Items))
	for _, item := range req.Items {
		if !isKnownKey(item.Key) {
			return nil, apperr.ErrValidation.Wrap(fmt.Errorf("未知的配置项: %q", item.Key))
		}
		if item.Value == "" && !item.Clear {
			// 空值且未显式要求清空 = 不修改，跳过这一项，不写入也不覆盖。
			continue
		}

		isSecret := item.Key.IsSecret()
		value := item.Value
		switch {
		case item.Clear:
			value = ""
		case isSecret:
			enc, err := crypto.Encrypt(value, string(item.Key))
			if err != nil {
				return nil, apperr.ErrInternal.Wrap(fmt.Errorf("加密配置项 %s 失败: %w", item.Key, err))
			}
			value = enc
		}

		entries = append(entries, settingmodel.Setting{
			Key:       string(item.Key),
			Value:     value,
			IsSecret:  isSecret,
			UpdatedBy: &actorID,
		})
	}

	if len(entries) > 0 {
		if err := s.settings.UpsertMany(ctx, entries); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx)
}

// TestConnection 用当前落库的凭证试调上游、验证它们确实有效。
// 缺少 dashscope_api_key 直接短路返回校验错误，不会打任何网络请求
// （也因此不会把"凭证根本没配"和"凭证配了但打不通"混成同一种错误）。
func (s *SettingService) TestConnection(ctx context.Context) error {
	apiKey, err := s.GetDecrypted(ctx, settingmodel.KeyDashscopeAPIKey)
	if err != nil {
		return err
	}
	if apiKey == "" {
		return apperr.ErrSettingIncomplete.Wrap(errors.New("dashscope_api_key 尚未配置"))
	}
	region, err := s.GetDecrypted(ctx, settingmodel.KeyRegion)
	if err != nil {
		return err
	}
	endpoint, err := s.GetDecrypted(ctx, settingmodel.KeyEndpoint)
	if err != nil {
		return err
	}
	workspaceID, err := s.GetDecrypted(ctx, settingmodel.KeyWorkspaceID)
	if err != nil {
		return err
	}

	return s.tester.Test(ctx, UpstreamCredentials{
		APIKey:      apiKey,
		Region:      region,
		Endpoint:    endpoint,
		WorkspaceID: workspaceID,
	})
}

// ── 默认 ConnectionTester 实现：直接打 DashScope compatible-mode ──────────
//
// 逻辑照抄 server.js 的 REGION_ENDPOINTS / getEndpoint / resolveEndpoint：
// 有自定义 endpoint（http 开头）优先用它；否则按 region 查表；
// eu-central-1 是模板化域名且强制要求 workspaceId；表里查不到的 region
// 兜底落回 cn-beijing——这三条规则与旧系统逐一对应，不是新发明的。

var dashScopeRegionEndpoints = map[string]string{
	"cn-beijing":     "https://dashscope.aliyuncs.com",
	"ap-southeast-1": "https://dashscope-intl.aliyuncs.com",
	"us-east-1":      "https://dashscope-us.aliyuncs.com",
}

func resolveDashScopeBaseURL(region, endpoint, workspaceID string) (string, error) {
	if strings.HasPrefix(endpoint, "http") {
		return strings.TrimRight(endpoint, "/"), nil
	}
	if region == "eu-central-1" {
		if workspaceID == "" {
			return "", errors.New("region 为 eu-central-1 时必须配置 workspace_id")
		}
		return fmt.Sprintf("https://%s.eu-central-1.maas.aliyuncs.com", workspaceID), nil
	}
	if base, ok := dashScopeRegionEndpoints[region]; ok {
		return base, nil
	}
	return dashScopeRegionEndpoints["cn-beijing"], nil
}

// connectionTestTimeout 探测请求的超时时间：比正常生成请求短得多，
// 因为探测只发一个 max_tokens=1 的最小请求，用来确认凭证有效，不是真的
// 要等一次完整生成。
const connectionTestTimeout = 15 * time.Second

type defaultConnectionTester struct{}

func (defaultConnectionTester) Test(ctx context.Context, creds UpstreamCredentials) error {
	base, err := resolveDashScopeBaseURL(creds.Region, creds.Endpoint, creds.WorkspaceID)
	if err != nil {
		return apperr.ErrSettingIncomplete.Wrap(err)
	}

	payload := map[string]any{
		"model":      "qwen-plus",
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return apperr.ErrInternal.Wrap(err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, connectionTestTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		base+"/compatible-mode/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return apperr.ErrInternal.Wrap(err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+creds.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: connectionTestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return apperr.ErrUpstreamTestFailed.Wrap(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	return apperr.ErrUpstreamTestFailed.Wrap(fmt.Errorf("上游返回状态码 %d", resp.StatusCode))
}
