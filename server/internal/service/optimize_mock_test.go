package service_test

import (
	"context"
	"fmt"

	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// fakeOptimizeSettings 是内存版 service.SettingReader，直接返回预设值，
// 不涉及数据库/加密——OptimizeService 测试只关心"解密后的明文被正确
// 传给了 provider 工厂"，不关心加解密本身（那是 SettingService 自己的
// 测试范围，见 setting_test.go）。
type fakeOptimizeSettings struct {
	values map[settingmodel.Key]string
	err    error
	// keyErr, when set, fails GetDecrypted only for the listed keys —
	// distinct from err (which fails every key uniformly). This exists to
	// prove a negative: "this code path never even reads key X" is only
	// testable by making X blow up if touched and asserting the call still
	// succeeds. See TestGenerateImage_GPTImage2_SucceedsEvenWhenRegionLookupErrors.
	keyErr map[settingmodel.Key]error
}

func newFakeOptimizeSettings(values map[settingmodel.Key]string) *fakeOptimizeSettings {
	return &fakeOptimizeSettings{values: values}
}

func (f *fakeOptimizeSettings) GetDecrypted(_ context.Context, key settingmodel.Key) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if e, ok := f.keyErr[key]; ok {
		return "", e
	}
	return f.values[key], nil
}

var _ service.SettingReader = (*fakeOptimizeSettings)(nil)

// optimizeProviderFunc 让一个普通函数满足 provider.OptimizeProvider，
// 免得每个测试都定义一个具名 struct。
type optimizeProviderFunc func(ctx context.Context, req provider.OptimizeRequest) (string, string, error)

func (f optimizeProviderFunc) Optimize(ctx context.Context, req provider.OptimizeRequest) (string, string, error) {
	return f(ctx, req)
}

var _ provider.OptimizeProvider = optimizeProviderFunc(nil)

// optimizeAttempt 记录一次上游调用尝试的 (model, endpoint 变体)，
// 用来断言降级链的尝试顺序与旧代码"模型外层、endpoint 内层"的嵌套循环
// 完全一致。endpoint 就是 service.ProviderFactory 收到的第四个参数——
// 配置覆盖值本身，或空字符串（代表"官方 endpoint"变体）。
type optimizeAttempt struct {
	model    string
	endpoint string
}

// scriptedResult 是某一次尝试应该返回的结果：err 非 nil 时代表这次调用
// 失败，text/model 只在 err 为 nil 时有意义。
type scriptedResult struct {
	text  string
	model string
	err   error
}

// scriptedProviderFactory 按调用顺序消费 script，记录每次尝试的
// (model, endpoint) 与完整的 provider.OptimizeRequest，供测试断言
// 降级链的尝试顺序、以及 sourceDesc/draftText/Images 的拼装结果。
type scriptedProviderFactory struct {
	script []scriptedResult

	calls int

	attempts     []optimizeAttempt
	capturedReqs []provider.OptimizeRequest

	// factoryEndpoints 记录每次 Factory() 返回的闭包被创建时传入的
	// endpoint 参数，与 attempts 里的 endpoint 应该逐一对应——用来确认
	// service 层确实是"每次尝试都重新调用一次工厂"，而不是复用同一个
	// provider 实例横跨不同 endpoint。
	factoryEndpoints []string
}

func newScriptedProviderFactory(script []scriptedResult) *scriptedProviderFactory {
	return &scriptedProviderFactory{script: script}
}

// Factory 返回可以直接传给 service.NewOptimizeServiceWithFactory 的
// service.ProviderFactory。
func (s *scriptedProviderFactory) Factory() service.ProviderFactory {
	return func(apiKey, region, workspaceID, endpoint string) provider.OptimizeProvider {
		s.factoryEndpoints = append(s.factoryEndpoints, endpoint)
		return optimizeProviderFunc(func(_ context.Context, req provider.OptimizeRequest) (string, string, error) {
			s.attempts = append(s.attempts, optimizeAttempt{model: req.Model, endpoint: endpoint})
			s.capturedReqs = append(s.capturedReqs, req)

			idx := s.calls
			s.calls++
			if idx >= len(s.script) {
				panic(fmt.Sprintf("scriptedProviderFactory: 第 %d 次调用没有对应的 script 条目（只准备了 %d 条）", idx, len(s.script)))
			}
			r := s.script[idx]
			if r.err != nil {
				return "", "", r.err
			}
			return r.text, r.model, nil
		})
	}
}
