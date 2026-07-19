package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/crypto"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
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
	settings      repository.SettingRepository
	tester        ConnectionTester
	encryptionKey []byte
}

// NewSettingService 构造生产用的 SettingService，TestConnection 走真正的
// dashscope.Client（见 dashscopeConnectionTester）。encryptionKey 必须是
// config.Config.AppEncryptionKey base64 解码后的 32 字节原始密钥——由 wire
// 的 provideEncryptionKey 在装配时从 *config.Config 解出并注入，
// SettingService 自己不读取任何配置来源。
func NewSettingService(settings repository.SettingRepository, encryptionKey []byte) *SettingService {
	return NewSettingServiceWithTester(settings, encryptionKey, newDashscopeConnectionTester())
}

// NewSettingServiceWithTester 允许调用方（生产环境接入真正的 provider 客户端、
// 测试环境注入假实现）指定 ConnectionTester。
func NewSettingServiceWithTester(settings repository.SettingRepository, encryptionKey []byte, tester ConnectionTester) *SettingService {
	return &SettingService{settings: settings, tester: tester, encryptionKey: encryptionKey}
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
		plain, err := s.decryptIfSecret(row)
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
	return s.decryptIfSecret(*row)
}

// decryptIfSecret 把一条可能是密文的 Setting 转成明文 Value。非 secret 项、
// 或 secret 项但当前是空字符串（未配置/已清空）原样返回，不尝试解密——
// crypto.Decrypt 只认 Encrypt 产出的 "iv:tag:ciphertext" 格式，拿空串去解会
// 直接报格式错误。
func (s *SettingService) decryptIfSecret(row settingmodel.Setting) (string, error) {
	if !row.IsSecret || row.Value == "" {
		return row.Value, nil
	}
	plain, err := crypto.Decrypt(s.encryptionKey, row.Value, row.Key)
	if err != nil {
		return "", apperr.ErrInternal.Wrap(fmt.Errorf("解密配置项 %s 失败: %w", row.Key, err))
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
			enc, err := crypto.Encrypt(s.encryptionKey, value, string(item.Key))
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

// ── 默认 ConnectionTester 实现：走真正的 dashscope.Client ────────────────
//
// Task 4 写下 defaultConnectionTester 时，internal/provider 还不存在，只能
// 手撸一份 REGION_ENDPOINTS/getEndpoint/resolveEndpoint 的照抄实现直接打
// DashScope compatible-mode。Task 6/8 之后 provider 层（含 dashscope.Client
// 的 baseURL() region 推导、eu-central-1 workspaceId 校验）已经落地，且
// OptimizeService 已经在用同一个 dashscope.Client 打同一条
// compatible-mode/chat-completions 路径——这里直接复用它，不再维护第二份
// region->endpoint 映射表。
//
// connectionTestTimeout 比正常生成请求短得多：探测只发一个最小 ping 请求，
// 用来确认凭证有效，不是要等一次完整生成/优化。

const connectionTestTimeout = 15 * time.Second

// dashscopeConnectionTester 用 ProviderFactory（optimize.go 定义，与
// OptimizeService 共用同一个生产实现 NewDashScopeOptimizeProviderFactory）
// 构造一个 dashscope.Client，通过它的 Optimize 方法探测凭证——Optimize 打的
// 正是 defaultConnectionTester 原来手写的那条 compatible-mode/chat
// completions 路径，且顺带验证了 region/endpoint/workspaceId 解析、鉴权头、
// 超时都是同一套代码在管，不会因为两份实现而漂移。
type dashscopeConnectionTester struct {
	factory ProviderFactory
}

func newDashscopeConnectionTester() dashscopeConnectionTester {
	return dashscopeConnectionTester{factory: NewDashScopeOptimizeProviderFactory()}
}

func (t dashscopeConnectionTester) Test(ctx context.Context, creds UpstreamCredentials) error {
	reqCtx, cancel := context.WithTimeout(ctx, connectionTestTimeout)
	defer cancel()

	p := t.factory(creds.APIKey, creds.Region, creds.WorkspaceID, creds.Endpoint)
	if _, _, err := p.Optimize(reqCtx, provider.OptimizeRequest{
		Model:        "qwen-plus",
		SystemPrompt: "connection test",
		UserText:     "ping",
	}); err != nil {
		return apperr.ErrUpstreamTestFailed.Wrap(err)
	}
	return nil
}
