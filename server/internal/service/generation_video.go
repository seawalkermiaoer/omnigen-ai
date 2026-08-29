// Package service — this file is the video task service: port of
// server.js's POST /api/create-task (server.js:306-323) and GET
// /api/task/:taskId (server.js:650-666). See
// docs/superpowers/plans/2026-07-19-generation-core.md, Task 12.
//
// Two behavioral differences from the old system, both deliberate:
//
//  1. Old server.js forwarded the client-supplied payload verbatim and let
//     the browser remember the upstream task_id (public/js/task.js:110
//     polls `/api/task/${taskId}` where taskId is literally the DashScope
//     task_id). This service persists a row server-side and hands back the
//     LOCAL row id — the upstream id lives only in Task.UpstreamTaskID,
//     never in the value CreateVideoTask returns to its caller. A caller
//     that accidentally used the upstream id as if it were the local id
//     would fail every subsequent GetTask/ListTasks call outright (they're
//     scoped by the local int64 id), rather than silently working by
//     coincidence.
//  2. Model/param validation against the catalog happens here, server-side,
//     before anything is sent upstream — mirroring generation_image.go.
//     The old system only checked this in the browser (public/js/{r2v,i2v}.js)
//     and server.js's `/api/create-task` never validated `model` at all
//     (see catalog.go's package doc for the asymmetry this fixes).
//
// The actual polling loop that advances a task from PENDING/RUNNING to a
// terminal status lives in internal/worker (Task 12's other half) — this
// file only creates the row and reads it back.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chenhao/omnigen-ai/server/internal/model/catalog"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// I2VTaskType is which of the three wan2.7-i2v task shapes a request uses —
// mirrors i2v.js's `i2v.state.task` values exactly (i2v.js:10,221).
// happyhorse-1.1-i2v (the non-wan i2v model) ignores this field entirely and
// always behaves like I2VTaskFirstFrame — see i2v.js:253-263, which builds
// its media array without ever consulting `i2v.state.task`.
type I2VTaskType string

const (
	I2VTaskFirstFrame I2VTaskType = "first_frame"
	I2VTaskFirstLast  I2VTaskType = "first_last"
	I2VTaskContinue   I2VTaskType = "continue"
)

// R2VMediaImage / R2VMediaVideo are one reference image / reference video
// entry in an r2v request. ReferenceVoice is optional in both — r2v.js:162
// gates it behind `wan && img.voiceUrl` for images (non-wan happyhorse never
// sends it even if the caller populated it — validateR2V rejects that
// combination instead of silently dropping it) and behind a plain
// `v.voiceUrl` truthiness check for videos (r2v.js:168; videos only exist at
// all on the wan branch to begin with, r2v.js:165).
//
// wan3.0 dropped reference_voice entirely (its media entries carry only
// type and url) — a wan3.0 request that sets ReferenceVoice is rejected,
// same rule and same reasoning as the happyhorse case.
type R2VMediaImage struct {
	URL            string
	ReferenceVoice string
}

type R2VMediaVideo struct {
	URL            string
	ReferenceVoice string
}

// R2VMediaAudio is one reference audio entry (type=reference_audio) — a
// wan3.0-only media kind with no wan2.7/happyhorse equivalent, which is why
// it has no ReferenceVoice field: the concept it would duplicate (a voice
// track attached to a reference) *is* what this media type already is.
type R2VMediaAudio struct {
	URL string
}

// VideoParams is the full set of parameters task.js's collectParams(kind)
// builds for every video request (task.js:304-315) plus the wan-only
// negative_prompt/prompt_extend this service also has to carry. Two
// different conventions live side by side here, matching the old code
// exactly:
//
//   - Resolution/Duration/Watermark are mandatory-with-a-default, never
//     optional: the old UI's <select>/<input> controls always have a
//     value (task.js:306-308 reads them unconditionally, and
//     collectWatermarkParams always returns `{watermark: enabled}` — a
//     checkbox is never "unset"). A caller that sends the zero value
//     (Resolution="", Duration=0, Watermark=false) gets exactly what the
//     old UI's defaults would have produced, not an omitted field.
//   - Seed/PromptExtend/NegativePrompt are genuinely optional — nil/""
//     means "the caller didn't provide this", and provider.ImageParams's
//     "nil means not provided" convention applies: seed=0 is a meaningful
//     explicit value once set, not absence.
//   - Ratio is optional-and-mode-gated: r2v/t2v default it to "16:9" when
//     empty (mirroring the <select id="ratio-r2v/t2v"> always having a
//     selected option); i2v must never receive one at all — see
//     normalizeVideoParams for why that's deliberate, not an oversight.
type VideoParams struct {
	NegativePrompt string
	PromptExtend   *bool
	Seed           *int64
	// Audio is wan3.0's "does the generated video have sound" switch. It
	// follows the genuinely-optional convention (nil = caller didn't
	// provide it, so the field is omitted and upstream's own default of
	// true applies) rather than Watermark's mandatory-with-a-default one,
	// because unlike watermark there is no old UI checkbox whose value is
	// always present — see validateOptionalVideoParams, which rejects it
	// outright on models whose catalog entry doesn't list ParamAudio.
	Audio *bool

	Resolution string
	Duration   int
	Ratio      string
	Watermark  bool
}

// CreateVideoTaskRequest is the service-level input for
// VideoGenerationService.CreateVideoTask. Only the fields relevant to the
// resolved mode (r2v: Images/Videos/Audios; i2v: TaskType/FirstFrame/
// LastFrame/FirstClip/DrivingAudio; f2v: FileURL; l2v: LinkURL; t2v: none
// of the above) are consulted, and any field belonging to a *different*
// mode is rejected rather than ignored — see buildVideoMedia.
//
// Mode used not to exist: before wan3.0 every video model in the catalog
// had exactly one video capability, so the mode could be derived from the
// model id and a separate field would only have been something to drift out
// of sync. wan3.0-video has five (t2v/i2v/r2v/f2v/l2v) behind a single id,
// so the mode has to come from the caller. It stays optional for
// single-capability models — resolveVideoMode infers it there, keeping
// every existing caller working unchanged — and is required as soon as the
// chosen model is ambiguous.
type CreateVideoTaskRequest struct {
	Model  string
	Prompt string

	// Mode is optional for models with exactly one video capability and
	// required for multi-capability ones (wan3.0). When set it must be one
	// of the model's own capabilities.
	Mode generationmodel.TaskMode

	Images []R2VMediaImage
	Videos []R2VMediaVideo
	Audios []R2VMediaAudio

	TaskType     I2VTaskType
	FirstFrame   string
	LastFrame    string
	FirstClip    string
	DrivingAudio string

	// FileURL / LinkURL are the single media entry for f2v (a document to
	// turn into a video) and l2v (a web page to turn into a video). Both
	// are wan3.0-only.
	FileURL string
	LinkURL string

	Params VideoParams
}

// VideoProviderFactory builds a provider.VideoProvider out of a set of
// credentials. Unlike ImageProviderFactory there is no protocol branch:
// video generation only ever goes through DashScope (provider.go's
// VideoProvider doc: "t8star 不实现这个接口——旧系统里视频生成只走
// DashScope"), so the factory always returns a *dashscope.Client. It is
// still a factory (not a bare *dashscope.Client field) so tests can inject a
// scripted provider.VideoProvider without a real network.
type VideoProviderFactory func(apiKey, region, workspaceID, endpoint string) provider.VideoProvider

// NewDashScopeVideoProviderFactory is the production factory.
func NewDashScopeVideoProviderFactory() VideoProviderFactory {
	return func(apiKey, region, workspaceID, endpoint string) provider.VideoProvider {
		return dashscope.New(apiKey, region, workspaceID, endpoint)
	}
}

// VideoGenerationService carries the business rules for POST
// /api/generate/video plus the read side (GET /api/tasks, GET
// /api/tasks/:id). Creating a task is asynchronous — the upstream call only
// hands back a task id to poll; the row lands as PENDING and is advanced by
// internal/worker.Poller, not by this service.
//
// 配额在提交时扣、失败时当场退——同步的一半（upstream 调用本身失败）与
// generation_image.go 完全对称。异步的一半（PENDING 任务后续被判定
// FAILED/超时）不归这里管：那是 internal/worker.Poller 通过
// repository.TaskRepository.RefundQuotaForTask 完成的，因为只有 worker
// 才知道一个 PENDING 任务最终有没有成功。
type VideoGenerationService struct {
	settings SettingReader
	tasks    repository.TaskRepository
	factory  VideoProviderFactory
	quota    *QuotaService
}

// NewVideoGenerationService constructs the production service, wired to the
// real DashScope client.
func NewVideoGenerationService(settings SettingReader, tasks repository.TaskRepository, quota *QuotaService) *VideoGenerationService {
	return NewVideoGenerationServiceWithFactory(settings, tasks, quota, NewDashScopeVideoProviderFactory())
}

// NewVideoGenerationServiceWithFactory allows tests to inject a fake
// VideoProviderFactory — no real network, no real database (tasks is an
// interface too).
func NewVideoGenerationServiceWithFactory(settings SettingReader, tasks repository.TaskRepository, quota *QuotaService, factory VideoProviderFactory) *VideoGenerationService {
	return &VideoGenerationService{settings: settings, tasks: tasks, factory: factory, quota: quota}
}

// CreateVideoTask validates against the catalog, fetches credentials, calls
// upstream to create the async task, and persists a PENDING row. It returns
// the persisted *generationmodel.Task — callers must use task.ID (the local
// row id), never task.UpstreamTaskID, to address this task in any later
// GetTask/ListTasks call.
func (s *VideoGenerationService) CreateVideoTask(
	ctx context.Context, userID int64, req CreateVideoTaskRequest,
) (task *generationmodel.Task, retErr error) {
	model, ok := catalog.ByID(req.Model)
	if !ok {
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 未知的模型 %q", req.Model))
	}

	mode, err := resolveVideoMode(model, req.Mode)
	if err != nil {
		return nil, err
	}

	media, err := buildVideoMedia(model, mode, req)
	if err != nil {
		return nil, err
	}

	params, err := normalizeVideoParams(model, mode, req.Params)
	if err != nil {
		return nil, err
	}
	if err := validateOptionalVideoParams(model, params); err != nil {
		return nil, err
	}

	region, err := s.settings.GetDecrypted(ctx, settingmodel.KeyRegion)
	if err != nil {
		return nil, err
	}
	if !model.AllowsRegion(region) {
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不允许在 region %q 下调用", req.Model, region))
	}

	apiKey, err := s.settings.GetDecrypted(ctx, settingmodel.KeyDashscopeAPIKey)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, apperr.ErrSettingIncomplete.Wrap(fmt.Errorf("generation_video: %s 尚未配置", settingmodel.KeyDashscopeAPIKey))
	}
	// 视频生成只走 DashScope（VideoProviderFactory 的文档注释），所以只读
	// dashscope_endpoint，不涉及 t8star_endpoint。
	endpoint, err := s.settings.GetDecrypted(ctx, settingmodel.KeyDashscopeEndpoint)
	if err != nil {
		return nil, err
	}
	workspaceID, err := s.settings.GetDecrypted(ctx, settingmodel.KeyWorkspaceID)
	if err != nil {
		return nil, err
	}

	// 扣费点：所有校验之后、真正调上游之前——同 generation_image.go。校验
	// 失败不该动额度；而一旦要发请求就必须先扣，否则并发下会超发。见
	// docs/superpowers/specs/2026-07-19-quota-and-stats-design.md「扣减点与退回点」。
	if err := s.quota.Consume(ctx, userID); err != nil {
		return nil, err
	}
	charged := true
	// defer 是兜底，不是主路径：失败分支会显式退款并把 charged 置 false，
	// 因为 defer 在 return 之后才运行，来不及影响即将创建的 task 行的
	// QuotaCharged 字段。panic 单独判断：panic 展开时函数根本不会执行到
	// 任何 return 语句，命名返回值 retErr 保持零值 nil，只判断
	// retErr != nil 会完全漏掉这条路径——见 generation_image.go 同一处的
	// 详细注释，这里的道理完全一样。
	defer func() {
		r := recover()
		if charged && (retErr != nil || r != nil) {
			if refundErr := s.quota.Refund(ctx, userID); refundErr != nil {
				slog.Error("退回额度失败", "userID", userID, "error", refundErr)
			}
			charged = false
		}
		if r != nil {
			panic(r)
		}
	}()

	payload := buildVideoPayload(model, mode, req, media, params)

	p := s.factory(apiKey, region, workspaceID, endpoint)
	upstreamID, callErr := p.CreateVideoTask(ctx, provider.VideoRequest{Payload: payload})
	// 上游状态归一化：同 generation_image.go，见 upstream_error.go 顶部注释。
	callErr = normalizeUpstreamError(callErr)

	task = &generationmodel.Task{
		UserID:    userID,
		Mode:      mode,
		Model:     req.Model,
		Prompt:    req.Prompt,
		Params:    videoParamsToStorageMap(params),
		InputURLs: mediaURLs(media),
	}

	if callErr != nil {
		// 显式退款并翻转 charged：这条 FAILED 行必须以 QuotaCharged=false
		// 落库，而 defer 运行得太晚，赶不上这次 Create。task.QuotaCharged
		// 是从 charged 派生的（不是硬编码 false），同 generation_image.go。
		if refundErr := s.quota.Refund(ctx, userID); refundErr != nil {
			slog.Error("退回额度失败", "userID", userID, "error", refundErr)
		}
		charged = false

		code, msg := errorCodeAndMessage(callErr)
		task.Status = generationmodel.StatusFailed
		task.ErrorCode = &code
		task.ErrorMessage = &msg
		task.QuotaCharged = charged
		if createErr := s.tasks.Create(ctx, task); createErr != nil {
			// 落库失败不能掩盖真正的上游失败原因——调用方需要看到的是
			// callErr（比如"上游拒绝"），不是这个次生的数据库错误。
			slog.Error("generation_video: 落库 FAILED 任务失败", "userID", userID, "model", req.Model, "err", createErr)
		}
		return nil, callErr
	}

	task.Status = generationmodel.StatusPending
	task.UpstreamTaskID = &upstreamID
	task.QuotaCharged = charged
	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

// GetTask fetches a single task, scoped to userID — a task belonging to a
// different user is indistinguishable from a task that doesn't exist at all
// (apperr.ErrTaskNotFound in both cases; see repository/task.go's
// GetByIDForUser doc for why that's deliberate).
func (s *VideoGenerationService) GetTask(ctx context.Context, userID, id int64) (*generationmodel.Task, error) {
	return s.tasks.GetByIDForUser(ctx, id, userID)
}

// ListTasks returns a user's own tasks, paginated.
func (s *VideoGenerationService) ListTasks(ctx context.Context, userID int64, q generationmodel.ListQuery) ([]generationmodel.Task, int64, error) {
	return s.tasks.ListForUser(ctx, userID, q.Offset(), q.Limit())
}

// DeleteTask deletes a single task, scoped to userID — same not-found-vs-
// forbidden rule as GetTask: another user's task and a nonexistent id are
// both apperr.ErrTaskNotFound.
//
// Deletion is allowed regardless of the task's current status, including
// PENDING/RUNNING (still in flight and possibly mid-poll in
// internal/worker.Poller). The alternative — refusing to delete an
// in-flight task — would force a "clear history" click to leave rows behind
// with no way to remove them until the upstream call eventually finishes or
// times out (up to maxTaskAge=30min later); that's a worse experience than
// the small risk accepted here. The row disappearing mid-poll is safe:
// TaskRepository.UpdateStatus/UpdateResult both return
// apperr.ErrTaskNotFound when the WHERE id=... matches zero rows (see
// task.go), and Poller.pollTask only logs that error via slog.Error — it
// never retries by re-creating the row, so a deleted in-flight task simply
// stops being written to on its next poll and is never resurrected. The one
// residual cost is a stale entry left behind in Poller.failureCounts for
// that task id (a plain int in a map, harmless and bounded by how many
// distinct tasks a long-running process ever polls) — not worth the extra
// bookkeeping to prune eagerly.
func (s *VideoGenerationService) DeleteTask(ctx context.Context, userID, id int64) error {
	return s.tasks.DeleteForUser(ctx, id, userID)
}

// DeleteAllTasks clears every task owned by userID and reports how many
// rows were removed, so the caller (history page's "清空全部") can confirm
// the count to the user. Same in-flight-deletion policy as DeleteTask.
func (s *VideoGenerationService) DeleteAllTasks(ctx context.Context, userID int64) (int64, error) {
	return s.tasks.DeleteAllForUser(ctx, userID)
}

// resolveVideoMode decides which of t2v/i2v/r2v/f2v/l2v this request is,
// from the model's catalog capabilities plus the caller's optional Mode.
//
// Two rules, and the asymmetry between them is the point:
//
//   - Model has exactly one video capability (every pre-wan3.0 video model):
//     Mode may be omitted and is inferred. This is what keeps every existing
//     caller — and every existing test — working untouched.
//   - Model has more than one (wan3.0): Mode is required. Guessing from
//     which request fields happen to be populated would make "I forgot to
//     attach the first frame" silently turn an i2v request into a t2v one
//     and bill the user for a video they didn't ask for.
//
// A Mode the model doesn't have is always an error, never silently
// downgraded — including for single-capability models, where sending
// mode=r2v to a t2v-only model is a caller bug worth surfacing.
func resolveVideoMode(model catalog.Model, requested generationmodel.TaskMode) (generationmodel.TaskMode, error) {
	caps := model.VideoCapabilities()
	if len(caps) == 0 {
		return "", apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不具备任何视频生成能力", model.ID))
	}

	if requested == "" {
		if len(caps) == 1 {
			return generationmodel.TaskMode(caps[0]), nil
		}
		return "", apperr.ErrValidation.Wrap(fmt.Errorf(
			"generation_video: 模型 %q 同时支持 %s，必须显式指定 mode", model.ID, joinCapabilities(caps)))
	}

	// Capability 与 TaskMode 在这五个取值上是同一套字符串（"t2v"/"i2v"/
	// "r2v"/"f2v"/"l2v"），所以可以直接转换比较——两个类型分别定义是为了
	// 表达"模型能做什么"与"这条记录是什么"的语义差别，不是取值差别。
	if !model.HasCapability(catalog.Capability(requested)) {
		return "", apperr.ErrValidation.Wrap(fmt.Errorf(
			"generation_video: 模型 %q 不支持 mode=%q，可用的是 %s", model.ID, requested, joinCapabilities(caps)))
	}
	return requested, nil
}

func joinCapabilities(caps []catalog.Capability) string {
	names := make([]string, 0, len(caps))
	for _, c := range caps {
		names = append(names, string(c))
	}
	return strings.Join(names, "/")
}

// buildVideoMedia returns the ordered media array for the upstream payload
// given an already-resolved mode. It validates required-field presence (and
// rejects fields belonging to other modes) but does not touch VideoParams —
// that's normalizeVideoParams' job, kept separate because it applies
// uniformly across every mode.
//
// The second dispatch axis is the model's VideoProfile: the same mode means
// a different media array on happyhorse, wan2.7 and wan3.0. Everything
// wan3.0-specific lives in the buildWan30* functions below rather than as
// `if` branches inside the wan2.7 ones, because the two generations share
// almost no rules — wan3.0 has no reference_voice, no first_clip, and
// counts each media type against its own limit instead of a combined one.
func buildVideoMedia(model catalog.Model, mode generationmodel.TaskMode, req CreateVideoTaskRequest) ([]map[string]any, error) {
	isWan30 := model.VideoProfile == catalog.VideoProfileWan30

	switch mode {
	case generationmodel.TaskModeT2V:
		if req.Prompt == "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: t2v 需要 prompt"))
		}
		return nil, nil
	case generationmodel.TaskModeR2V:
		if isWan30 {
			return buildWan30R2VMedia(model, req)
		}
		return buildR2VRequest(model, req)
	case generationmodel.TaskModeI2V:
		if isWan30 {
			return buildWan30I2VMedia(req)
		}
		return buildI2VRequest(model, req)
	case generationmodel.TaskModeF2V:
		if req.FileURL == "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: f2v 需要一个文件 URL"))
		}
		return []map[string]any{{"type": "file", "url": req.FileURL}}, nil
	case generationmodel.TaskModeL2V:
		if req.LinkURL == "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: l2v 需要一个网页链接"))
		}
		return []map[string]any{{"type": "link", "url": req.LinkURL}}, nil
	default:
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 未知的视频模式 %q", mode))
	}
}

// buildWan30I2VMedia ports the 首帧生视频 / 首尾帧生视频 request shapes from
// 万相3.0视频生成API参考. TaskType stays the caller-facing switch (same three
// constants as wan2.7 so the UI's task-type tabs work identically), but
// wan3.0 only has two of them:
//
//   - first_frame: [first_frame]
//   - first_last:  [first_frame, last_frame]
//   - continue:    rejected — wan3.0 has no first_clip media type at all.
//     Extending an existing video is an r2v request there (a reference_video
//     plus a prompt describing the continuation), so this returns an error
//     naming that route instead of a bare "unsupported": a caller hitting
//     this has a real use case, it just moved.
//
// driving_audio is likewise gone; a wan3.0 request carrying one is rejected
// rather than dropped, so nobody ships a "why is my audio ignored" bug.
func buildWan30I2VMedia(req CreateVideoTaskRequest) ([]map[string]any, error) {
	if req.FirstClip != "" || req.TaskType == I2VTaskContinue {
		return nil, apperr.ErrValidation.Wrap(errors.New(
			"generation_video: wan3.0 没有续接片段任务，视频延长请改用参考生视频（参考视频 + 描述延长内容的 prompt）"))
	}
	if req.DrivingAudio != "" {
		return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: wan3.0 不支持 driving_audio"))
	}
	if req.FirstFrame == "" {
		return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: i2v 需要首帧图片"))
	}

	// TaskType 为空按 first_frame 处理——wan3.0 的 last_frame 在上游本就是
	// 可选的，调用方不指定任务类型时用"有没有传尾帧"来决定即可；显式
	// 指定 first_last 却不传尾帧则是矛盾请求，必须报错而不是降级。
	switch req.TaskType {
	case "", I2VTaskFirstFrame:
		if req.LastFrame != "" && req.TaskType == I2VTaskFirstFrame {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_frame 任务不接受尾帧"))
		}
	case I2VTaskFirstLast:
		if req.LastFrame == "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_last 任务需要尾帧图片"))
		}
	default:
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 未知的 i2v 任务类型 %q", req.TaskType))
	}

	media := []map[string]any{{"type": "first_frame", "url": req.FirstFrame}}
	if req.LastFrame != "" {
		media = append(media, map[string]any{"type": "last_frame", "url": req.LastFrame})
	}
	return media, nil
}

// buildWan30R2VMedia ports 参考生视频 (and, with a reference_video plus an
// editing/extension prompt, 视频编辑 / 视频延长 — the same request shape,
// distinguished only by what the prompt asks for, per the API reference).
//
// Unlike wan2.7's single combined cap (images + videos <= 5), wan3.0 limits
// each media type separately (10 / 5 / 5), which is what catalog.MediaLimits
// records. reference_voice does not exist here and is rejected rather than
// dropped.
//
// Media order follows the caller's order within each type, images then
// videos then audios. The API reference's own example interleaves images and
// videos, so order across types is not significant to the model; keeping it
// grouped and deterministic is what makes InputURLs (and therefore the
// history page) reproducible.
func buildWan30R2VMedia(model catalog.Model, req CreateVideoTaskRequest) ([]map[string]any, error) {
	for _, img := range req.Images {
		if img.ReferenceVoice != "" {
			return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不支持 reference_voice", model.ID))
		}
	}
	for _, v := range req.Videos {
		if v.ReferenceVoice != "" {
			return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不支持 reference_voice", model.ID))
		}
	}

	if len(req.Images)+len(req.Videos)+len(req.Audios) == 0 {
		return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: r2v 需要至少一个参考图/参考视频/参考音频"))
	}

	limits := model.MediaLimits
	for _, chk := range []struct {
		n     int
		max   int
		label string
	}{
		{len(req.Images), limits.ReferenceImages, "参考图"},
		{len(req.Videos), limits.ReferenceVideos, "参考视频"},
		{len(req.Audios), limits.ReferenceAudios, "参考音频"},
	} {
		if chk.max > 0 && chk.n > chk.max {
			return nil, apperr.ErrValidation.Wrap(fmt.Errorf(
				"generation_video: %s数量 %d 超过模型 %q 的上限 %d", chk.label, chk.n, model.ID, chk.max))
		}
	}

	media := make([]map[string]any, 0, len(req.Images)+len(req.Videos)+len(req.Audios))
	for _, img := range req.Images {
		media = append(media, map[string]any{"type": "reference_image", "url": img.URL})
	}
	for _, v := range req.Videos {
		media = append(media, map[string]any{"type": "reference_video", "url": v.URL})
	}
	for _, a := range req.Audios {
		media = append(media, map[string]any{"type": "reference_audio", "url": a.URL})
	}
	return media, nil
}

// buildR2VRequest ports r2v.js:138-186 for the pre-wan3.0 models. isWan
// mirrors r2v.js's `isWanR2v()` (a literal model-id compare); it now reads
// the catalog's explicit VideoProfile rather than the old
// `len(model.Regions) > 0` proxy — see catalog.VideoProfile's doc for why
// that proxy stopped working.
func buildR2VRequest(model catalog.Model, req CreateVideoTaskRequest) ([]map[string]any, error) {
	if req.Prompt == "" {
		return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: r2v 需要 prompt"))
	}
	// reference_audio 是 wan3.0 才有的媒体类型，这条分支上的模型都不认识它。
	if len(req.Audios) > 0 {
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不支持参考音频", model.ID))
	}

	isWan := model.VideoProfile == catalog.VideoProfileWan27
	if !isWan {
		if len(req.Videos) > 0 {
			return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不支持参考视频", model.ID))
		}
		for _, img := range req.Images {
			if img.ReferenceVoice != "" {
				return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不支持 reference_voice", model.ID))
			}
		}
	}

	total := len(req.Images) + len(req.Videos)
	if total == 0 {
		return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: r2v 需要至少一张参考图或一个参考视频"))
	}
	if model.MaxImages > 0 && total > model.MaxImages {
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 参考媒体数量 %d 超过模型 %q 的上限 %d", total, model.ID, model.MaxImages))
	}

	return buildR2VMedia(req.Images, req.Videos), nil
}

// buildI2VRequest ports i2v.js:212-263. Non-wan (happyhorse-1.1-i2v) always
// behaves as first_frame regardless of the caller-supplied TaskType — the
// old UI never even exposes task-type tabs for that model (i2v.js only
// switches media assembly logic on `wan`, not on `task`, for the non-wan
// branch; see i2v.js:253-263).
func buildI2VRequest(model catalog.Model, req CreateVideoTaskRequest) ([]map[string]any, error) {
	isWan := model.VideoProfile == catalog.VideoProfileWan27

	if !isWan {
		if req.FirstFrame == "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: i2v 需要首帧图片"))
		}
		if req.LastFrame != "" || req.FirstClip != "" || req.DrivingAudio != "" {
			return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 只支持首帧图片", model.ID))
		}
		return buildI2VMedia(I2VTaskFirstFrame, req.FirstFrame, "", "", ""), nil
	}

	switch req.TaskType {
	case I2VTaskFirstFrame:
		if req.FirstFrame == "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_frame 任务需要首帧图片"))
		}
		if req.LastFrame != "" || req.FirstClip != "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_frame 任务不接受尾帧/续接片段"))
		}
	case I2VTaskFirstLast:
		if req.FirstFrame == "" || req.LastFrame == "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_last 任务需要首帧与尾帧图片"))
		}
		if req.FirstClip != "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_last 任务不接受续接片段"))
		}
	case I2VTaskContinue:
		if req.FirstClip == "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: continue 任务需要续接片段"))
		}
		if req.FirstFrame != "" {
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: continue 任务不接受首帧图片"))
		}
		if req.DrivingAudio != "" {
			// i2v.js:236 只在 first_frame/first_last 任务上附加
			// driving_audio；continue 任务的 UI 从不展示这个输入框。
			return nil, apperr.ErrValidation.Wrap(errors.New("generation_video: continue 任务不接受 driving_audio"))
		}
	default:
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 未知的 i2v 任务类型 %q", req.TaskType))
	}

	return buildI2VMedia(req.TaskType, req.FirstFrame, req.LastFrame, req.FirstClip, req.DrivingAudio), nil
}

// buildR2VMedia ports r2v.js:159-171 exactly: all reference images first
// (in caller order), then all reference videos (in caller order).
// reference_voice is only attached when non-empty — omitted, not sent as an
// empty string, matching `if (wan && img.voiceUrl) m.reference_voice = ...`.
func buildR2VMedia(images []R2VMediaImage, videos []R2VMediaVideo) []map[string]any {
	media := make([]map[string]any, 0, len(images)+len(videos))
	for _, img := range images {
		item := map[string]any{"type": "reference_image", "url": img.URL}
		if img.ReferenceVoice != "" {
			item["reference_voice"] = img.ReferenceVoice
		}
		media = append(media, item)
	}
	for _, v := range videos {
		item := map[string]any{"type": "reference_video", "url": v.URL}
		if v.ReferenceVoice != "" {
			item["reference_voice"] = v.ReferenceVoice
		}
		media = append(media, item)
	}
	return media
}

// buildI2VMedia ports i2v.js:221-238 exactly, including the ordering that
// differs per task type:
//
//   - first_frame:  [first_frame] (+ driving_audio if set)
//   - first_last:   [first_frame, last_frame] (+ driving_audio if set)
//   - continue:     [first_clip] (+ last_frame if set — an *optional
//     trailing constraint* here, unlike first_last where it's required)
//
// driving_audio only ever applies to the two frame-based types — continue
// never gets one, matching i2v.js:236's condition.
func buildI2VMedia(taskType I2VTaskType, firstFrame, lastFrame, firstClip, drivingAudio string) []map[string]any {
	media := make([]map[string]any, 0, 3)

	if taskType == I2VTaskFirstFrame || taskType == I2VTaskFirstLast {
		media = append(media, map[string]any{"type": "first_frame", "url": firstFrame})
	}
	if taskType == I2VTaskFirstLast {
		media = append(media, map[string]any{"type": "last_frame", "url": lastFrame})
	}
	if taskType == I2VTaskContinue {
		media = append(media, map[string]any{"type": "first_clip", "url": firstClip})
		if lastFrame != "" {
			media = append(media, map[string]any{"type": "last_frame", "url": lastFrame})
		}
	}
	if (taskType == I2VTaskFirstFrame || taskType == I2VTaskFirstLast) && drivingAudio != "" {
		media = append(media, map[string]any{"type": "driving_audio", "url": drivingAudio})
	}

	return media
}

// normalizeVideoParams applies task.js:304-315's defaulting rules and
// rejects anything outside the allowed value sets. It must run before
// validateOptionalVideoParams/buildVideoPayload — both downstream consumers
// assume Resolution/Duration are already valid and defaulted, and that
// Ratio is empty exactly when it must not be sent.
//
// The allowed sets come from the model's catalog entry, not from constants
// in this file. They used to be package-level constants here (and a
// duplicate copy in the frontend's videoParams.ts) on the grounds that
// every video model shared the same values — true right up until wan3.0,
// which allows 480P, runs 2–30s instead of 3–15s, and has a different
// ratio set including "adaptive". Rather than add a per-generation branch
// in two places, the sets moved into catalog.Model next to Sizes/MaxN,
// where the rest of the "what does this model accept" data already lives.
//
// resolution/duration/watermark are mandatory-with-a-default: the old UI's
// controls always have a value, so a caller that omits them gets the same
// default the old UI would have produced, not an error. Watermark has no
// validation branch at all — every bool value is valid, so "not set"
// (false) and "explicitly false" are indistinguishable and that's fine,
// matching collectWatermarkParams' unconditional `{watermark: enabled}`.
//
// ratio is where old and new behavior are the same on purpose but for
// different reasons. In task.js, ratio is only collected `if (ratioEl)`
// (task.js:310-311) — the element lookup itself is what gates it, and for
// i2v that lookup fails because the i2v panel never had a `#ratio-i2v`
// element to begin with (index.html has `#autoRatio-i2v`, a disabled input
// labeled "自动跟随首帧" / "follows the first frame automatically" —
// index.html:251). So the old system's ratio omission for i2v was an
// accident of DOM lookup, not a deliberate design decision documented
// anywhere. The resulting behavior — i2v derives its aspect ratio from the
// first frame and never gets a ratio parameter — is nonetheless correct for
// those models, so this function makes it deliberate: when the model's
// catalog entry sets I2VAutoRatio, an i2v request that carries a Ratio at
// all is rejected outright (silently dropping a caller-supplied parameter
// is how you get a bug report when someone "fixes" the missing UI field six
// months from now). wan3.0 does not set that flag: its ratio applies in
// every mode, and its default "adaptive" *is* the "follow the input"
// behavior, so there is nothing to suppress.
func normalizeVideoParams(model catalog.Model, mode generationmodel.TaskMode, p VideoParams) (VideoParams, error) {
	out := p

	switch {
	case out.Resolution == "":
		out.Resolution = model.DefaultResolution
	case !model.AllowsResolution(out.Resolution):
		return VideoParams{}, apperr.ErrValidation.Wrap(fmt.Errorf(
			"generation_video: 模型 %q 不支持的 resolution %q（可用：%s）",
			model.ID, out.Resolution, strings.Join(model.Resolutions, "、")))
	}

	switch {
	case out.Duration == 0:
		out.Duration = model.DefaultDuration
	case !model.AllowsDuration(out.Duration):
		return VideoParams{}, apperr.ErrValidation.Wrap(fmt.Errorf(
			"generation_video: 模型 %q 的 duration=%d 超出允许范围 [%d,%d]",
			model.ID, out.Duration, model.DurationMin, model.DurationMax))
	}

	if mode == generationmodel.TaskModeI2V && model.I2VAutoRatio {
		if out.Ratio != "" {
			return VideoParams{}, apperr.ErrValidation.Wrap(errors.New("generation_video: i2v 不接受 ratio，宽高比由首帧决定"))
		}
		return out, nil
	}

	switch {
	case out.Ratio == "":
		out.Ratio = model.DefaultRatio
	case !model.AllowsRatio(out.Ratio):
		return VideoParams{}, apperr.ErrValidation.Wrap(fmt.Errorf(
			"generation_video: 模型 %q 不支持的 ratio %q（可用：%s）",
			model.ID, out.Ratio, strings.Join(model.Ratios, "、")))
	}
	return out, nil
}

// validateOptionalVideoParams rejects any genuinely-optional parameter the
// model's catalog entry doesn't list in Supports — mirrors
// validateImageRequest's `unsupported` loop in generation_image.go.
// Resolution/Duration/Ratio/Watermark are not in scope here: they're not
// catalog-gated (see normalizeVideoParams's package doc above), they're
// mode-gated (Ratio) or always allowed (the rest).
func validateOptionalVideoParams(model catalog.Model, p VideoParams) error {
	checks := []struct {
		set   bool
		param string
	}{
		{p.NegativePrompt != "", catalog.ParamNegativePrompt},
		{p.PromptExtend != nil, catalog.ParamPromptExtend},
		{p.Seed != nil, catalog.ParamSeed},
		{p.Audio != nil, catalog.ParamAudio},
	}
	for _, chk := range checks {
		if chk.set && !model.SupportsParam(chk.param) {
			return apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不支持参数 %s", model.ID, chk.param))
		}
	}
	return nil
}

// buildVideoPayload assembles the full video-synthesis request body. params
// must already be normalized (normalizeVideoParams) — Resolution/Duration
// are assumed valid-and-defaulted, and Ratio is assumed empty exactly when
// it shouldn't be sent (i2v).
//
// negative_prompt is placed on `input`, not `parameters` — r2v.js:178
// (`input.negative_prompt = negative`) and i2v.js:244 do this identically;
// it is not a sibling of prompt_extend/seed/watermark despite living in the
// same VideoParams struct here.
//
// prompt placement differs by mode: r2v always includes it (r2v.js:174
// `input = { prompt, media }`, unconditional even though prompt is
// required and thus never empty in practice); i2v/t2v only include it when
// non-empty (i2v.js:242 `if (prompt) input.prompt = prompt`).
//
// resolution/duration/watermark are always present in `parameters` — never
// omitted, matching task.js:304-315's unconditional emission (this is also
// why watermark=false must never be dropped: task.js sends it exactly as
// often as it sends true). ratio is only present when params.Ratio is
// non-empty, which after normalizeVideoParams means "r2v/t2v" — i2v's Ratio
// is guaranteed empty at this point, so the `if` below is what actually
// keeps ratio off the wire for i2v, not a mode check duplicated here.
func buildVideoPayload(model catalog.Model, mode generationmodel.TaskMode, req CreateVideoTaskRequest, media []map[string]any, params VideoParams) map[string]any {
	input := map[string]any{}
	if len(media) > 0 {
		input["media"] = media
	}
	if mode == generationmodel.TaskModeR2V {
		input["prompt"] = req.Prompt
	} else if req.Prompt != "" {
		input["prompt"] = req.Prompt
	}
	if params.NegativePrompt != "" {
		input["negative_prompt"] = params.NegativePrompt
	}

	parameters := map[string]any{
		"resolution": params.Resolution,
		"duration":   params.Duration,
		"watermark":  params.Watermark,
	}
	if params.Ratio != "" {
		parameters["ratio"] = params.Ratio
	}
	if params.PromptExtend != nil {
		parameters["prompt_extend"] = *params.PromptExtend
	}
	if params.Seed != nil {
		parameters["seed"] = *params.Seed
	}
	if params.Audio != nil {
		parameters["audio"] = *params.Audio
	}

	return map[string]any{
		"model":      model.ID,
		"input":      input,
		"parameters": parameters,
	}
}

// mediaURLs extracts just the URLs (in payload order) for InputURLs —
// the task row keeps them for history/display, independent of the
// type/reference_voice detail that only matters for the upstream call.
func mediaURLs(media []map[string]any) []string {
	urls := make([]string, 0, len(media))
	for _, m := range media {
		if u, ok := m["url"].(string); ok {
			urls = append(urls, u)
		}
	}
	return urls
}

// videoParamsToStorageMap mirrors paramsToStorageMap in generation_image.go
// for the genuinely-optional fields (only recorded when the caller set
// them); resolution/duration/watermark are always recorded since — after
// normalizeVideoParams — they always hold a real, defaulted value, not an
// absence.
func videoParamsToStorageMap(p VideoParams) map[string]any {
	m := map[string]any{
		"resolution": p.Resolution,
		"duration":   p.Duration,
		"watermark":  p.Watermark,
	}
	if p.Ratio != "" {
		m["ratio"] = p.Ratio
	}
	if p.NegativePrompt != "" {
		m["negativePrompt"] = p.NegativePrompt
	}
	if p.PromptExtend != nil {
		m["promptExtend"] = *p.PromptExtend
	}
	if p.Seed != nil {
		m["seed"] = *p.Seed
	}
	if p.Audio != nil {
		m["audio"] = *p.Audio
	}
	return m
}
