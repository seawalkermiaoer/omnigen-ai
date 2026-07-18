// Package service — this file is the image generation service: port of
// server.js's POST /api/generate-image (server.js:326-441). See
// docs/superpowers/plans/2026-07-19-generation-core.md, Task 11.
//
// Unlike the old system, params are validated against the model catalog
// (internal/model/catalog) *before* anything is sent upstream — the old
// system only checked most of these constraints in the browser
// (public/js/imggen.js / imgedit.js), so any client that skipped the UI
// (curl, a modified frontend build) could send a request the catalog would
// have refused, most notably wan2.7 with a region outside its whitelist.
// This file is the server-side gate that closes that hole.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
	"github.com/chenhao/omnigen-ai/server/internal/provider/t8star"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// GenerateImageRequest is the service-level input for
// ImageGenerationService.GenerateImage. Images being empty vs non-empty is
// what decides t2i vs edit — mirrors the old system, where imggen.js never
// attaches images and imgedit.js always does; there is no separate "mode"
// field to keep in sync with that fact.
type GenerateImageRequest struct {
	Model  string
	Prompt string
	Images []string
	Params provider.ImageParams
}

// ImageProviderFactory builds a provider.ImageProvider for a given model
// protocol out of a set of credentials. Both dashscope.Client and
// t8star.Client already implement provider.ImageProvider directly (t8star's
// wraps its Note into the returned provider.ImageResult), so the factory
// only needs to pick which constructor to call — no protocol-specific
// branching is needed anywhere else in this file.
type ImageProviderFactory func(protocol catalog.Protocol, apiKey, region, workspaceID, endpoint string) provider.ImageProvider

// NewDashScopeT8starImageProviderFactory is the production factory: OpenAI
// protocol models (t8star) get a t8star.Client, everything else
// (ProtocolDashScope) gets a dashscope.Client.
func NewDashScopeT8starImageProviderFactory() ImageProviderFactory {
	return func(protocol catalog.Protocol, apiKey, region, workspaceID, endpoint string) provider.ImageProvider {
		if protocol == catalog.ProtocolOpenAI {
			return t8star.New(apiKey, endpoint)
		}
		return dashscope.New(apiKey, region, workspaceID, endpoint)
	}
}

// ImageGenerationService 承载 POST /api/generate/image 的业务规则：目录校验、
// 凭证读取、按协议选 provider、同步调用并落库。图片生成是同步的——一次
// GenerateImage 调用要么落一行 SUCCEEDED、要么落一行 FAILED，不会有中间态。
type ImageGenerationService struct {
	settings SettingReader
	tasks    repository.TaskRepository
	factory  ImageProviderFactory
}

// NewImageGenerationService 构造生产用的 ImageGenerationService，provider
// 走真正的 dashscope/t8star 客户端。
func NewImageGenerationService(settings SettingReader, tasks repository.TaskRepository) *ImageGenerationService {
	return NewImageGenerationServiceWithFactory(settings, tasks, NewDashScopeT8starImageProviderFactory())
}

// NewImageGenerationServiceWithFactory 允许测试注入假的 ImageProviderFactory，
// 不必打真实网络请求，也不需要真实数据库（tasks 同样是接口，测试可以注入
// 内存假实现）。
func NewImageGenerationServiceWithFactory(settings SettingReader, tasks repository.TaskRepository, factory ImageProviderFactory) *ImageGenerationService {
	return &ImageGenerationService{settings: settings, tasks: tasks, factory: factory}
}

// GenerateImage 校验目录 → 取凭证 → 选 provider → 同步调用 → 落库。
//
// 成功与失败都会尝试落一行 generation_tasks；返回值上，成功时返回落库后的
// *generationmodel.Task（含数据库分配的 ID），失败时返回 nil 与那个错误——
// 调用方（handler）只需要照常把 err 交给 middleware.Fail，不需要关心
// "已经落了一行 FAILED" 这件事，那是审计/历史记录的关注点，不是这次请求
// 响应的一部分。
func (s *ImageGenerationService) GenerateImage(ctx context.Context, userID int64, req GenerateImageRequest) (*generationmodel.Task, error) {
	model, ok := catalog.ByID(req.Model)
	if !ok {
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_image: 未知的模型 %q", req.Model))
	}

	mode, err := validateImageRequest(model, req)
	if err != nil {
		return nil, err
	}

	region, err := s.settings.GetDecrypted(ctx, settingmodel.KeyRegion)
	if err != nil {
		return nil, err
	}
	if !model.AllowsRegion(region) {
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_image: 模型 %q 不允许在 region %q 下调用", req.Model, region))
	}

	apiKeyKey := settingmodel.KeyDashscopeAPIKey
	if model.Protocol == catalog.ProtocolOpenAI {
		apiKeyKey = settingmodel.KeyT8starAPIKey
	}
	apiKey, err := s.settings.GetDecrypted(ctx, apiKeyKey)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, apperr.ErrSettingIncomplete.Wrap(fmt.Errorf("generation_image: %s 尚未配置", apiKeyKey))
	}
	endpoint, err := s.settings.GetDecrypted(ctx, settingmodel.KeyEndpoint)
	if err != nil {
		return nil, err
	}
	workspaceID, err := s.settings.GetDecrypted(ctx, settingmodel.KeyWorkspaceID)
	if err != nil {
		return nil, err
	}

	p := s.factory(model.Protocol, apiKey, region, workspaceID, endpoint)
	result, callErr := p.GenerateImage(ctx, provider.ImageRequest{
		Model:  req.Model,
		Prompt: req.Prompt,
		Images: req.Images,
		Params: req.Params,
	})

	task := &generationmodel.Task{
		UserID:    userID,
		Mode:      mode,
		Model:     req.Model,
		Prompt:    req.Prompt,
		Params:    paramsToStorageMap(req.Params),
		InputURLs: req.Images,
	}

	if callErr != nil {
		code, msg := errorCodeAndMessage(callErr)
		task.Status = generationmodel.StatusFailed
		task.ErrorCode = &code
		task.ErrorMessage = &msg
		if createErr := s.tasks.Create(ctx, task); createErr != nil {
			// 落库失败不能掩盖真正的上游失败原因——调用方需要看到的是
			// callErr（比如"上游拒绝"），不是这个次生的数据库错误。
			slog.Error("generation_image: 落库 FAILED 任务失败", "userID", userID, "model", req.Model, "err", createErr)
		}
		return nil, callErr
	}

	task.Status = generationmodel.StatusSucceeded
	task.Model = result.Model
	task.ResultURLs = result.Images
	task.Usage = result.Usage
	if result.Note != "" {
		note := result.Note
		task.Note = &note
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

// validateImageRequest 把 catalog.Model 的全部参数约束过一遍：能力匹配、
// n（含 enable_sequential 时的上限切换）、size、图片数量、以及"这个模型认
// 不认这个可选参数"。任何一条不满足都返回 apperr.ErrValidation，不打任何
// 网络请求。
func validateImageRequest(model catalog.Model, req GenerateImageRequest) (generationmodel.TaskMode, error) {
	wantCap := catalog.CapabilityT2I
	mode := generationmodel.TaskModeImgGen
	if len(req.Images) > 0 {
		wantCap = catalog.CapabilityEdit
		mode = generationmodel.TaskModeImgEdit
	}
	if !model.HasCapability(wantCap) {
		return "", apperr.ErrValidation.Wrap(fmt.Errorf("generation_image: 模型 %q 不支持 %s", model.ID, wantCap))
	}

	if len(req.Images) > model.MaxImages {
		return "", apperr.ErrValidation.Wrap(fmt.Errorf("generation_image: 参考图数量 %d 超过模型 %q 的上限 %d", len(req.Images), model.ID, model.MaxImages))
	}

	sequential := req.Params.EnableSequential != nil && *req.Params.EnableSequential
	if sequential && model.SequentialMaxN == 0 {
		return "", apperr.ErrValidation.Wrap(fmt.Errorf("generation_image: 模型 %q 不支持 enable_sequential", model.ID))
	}
	maxN := model.MaxN
	if sequential {
		maxN = model.SequentialMaxN
	}
	if req.Params.N != nil {
		if n := *req.Params.N; n < 1 || n > maxN {
			return "", apperr.ErrValidation.Wrap(fmt.Errorf("generation_image: n=%d 超出模型 %q 的允许范围 [1,%d]", n, model.ID, maxN))
		}
	}

	if req.Params.Size != "" && !containsString(model.Sizes, req.Params.Size) {
		return "", apperr.ErrValidation.Wrap(fmt.Errorf("generation_image: 模型 %q 不支持 size %q", model.ID, req.Params.Size))
	}

	unsupported := []struct {
		set   bool
		param string
	}{
		{req.Params.ThinkingMode != nil, catalog.ParamThinkingMode},
		{req.Params.EnableSequential != nil, catalog.ParamEnableSequential},
		{req.Params.NegativePrompt != "", catalog.ParamNegativePrompt},
		{req.Params.PromptExtend != nil, catalog.ParamPromptExtend},
		{req.Params.Seed != nil, catalog.ParamSeed},
		{req.Params.Watermark != nil, catalog.ParamWatermark},
	}
	for _, chk := range unsupported {
		if chk.set && !model.SupportsParam(chk.param) {
			return "", apperr.ErrValidation.Wrap(fmt.Errorf("generation_image: 模型 %q 不支持参数 %s", model.ID, chk.param))
		}
	}

	return mode, nil
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// paramsToStorageMap 把请求里显式提供的可选参数转成 JSONB 落库的形状，
// 只收录调用方实际设置过的字段——与 dashscope 包 buildImageParameters 的
// 判空规则一致（Size/NegativePrompt 用真值判断，其余用指针非 nil 判断）。
func paramsToStorageMap(p provider.ImageParams) map[string]any {
	m := map[string]any{}
	if p.Size != "" {
		m["size"] = p.Size
	}
	if p.NegativePrompt != "" {
		m["negativePrompt"] = p.NegativePrompt
	}
	if p.N != nil {
		m["n"] = *p.N
	}
	if p.Watermark != nil {
		m["watermark"] = *p.Watermark
	}
	if p.Seed != nil {
		m["seed"] = *p.Seed
	}
	if p.ThinkingMode != nil {
		m["thinkingMode"] = *p.ThinkingMode
	}
	if p.EnableSequential != nil {
		m["enableSequential"] = *p.EnableSequential
	}
	if p.PromptExtend != nil {
		m["promptExtend"] = *p.PromptExtend
	}
	return m
}

// errorCodeAndMessage 从上游/provider 返回的 error 里抠出 (code, message)
// 落进 error_code/error_message 两列。*apperr.AppError 用它自己的 Code()
// 与 internal 原因；任何其它 error（理论上不应该发生，provider 层全部返回
// *apperr.AppError）兜底成 INTERNAL_ERROR，避免落库时 panic。
func errorCodeAndMessage(err error) (code, message string) {
	var appErr *apperr.AppError
	if errors.As(err, &appErr) {
		message = appErr.Code()
		if appErr.Internal() != nil {
			message = appErr.Internal().Error()
		}
		return appErr.Code(), message
	}
	return apperr.ErrInternal.Code(), err.Error()
}
