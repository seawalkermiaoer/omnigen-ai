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
type R2VMediaImage struct {
	URL            string
	ReferenceVoice string
}

type R2VMediaVideo struct {
	URL            string
	ReferenceVoice string
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

	Resolution string
	Duration   int
	Ratio      string
	Watermark  bool
}

// CreateVideoTaskRequest is the service-level input for
// VideoGenerationService.CreateVideoTask. Only the fields relevant to the
// resolved mode (r2v: Images/Videos; i2v: TaskType/FirstFrame/LastFrame/
// FirstClip/DrivingAudio; t2v: none of the above) are consulted — which
// fields matter is decided by the model's catalog capability, not by a
// caller-supplied mode flag, so there is no separate "mode" field here to
// drift out of sync with the model id.
type CreateVideoTaskRequest struct {
	Model  string
	Prompt string

	Images []R2VMediaImage
	Videos []R2VMediaVideo

	TaskType     I2VTaskType
	FirstFrame   string
	LastFrame    string
	FirstClip    string
	DrivingAudio string

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
type VideoGenerationService struct {
	settings SettingReader
	tasks    repository.TaskRepository
	factory  VideoProviderFactory
}

// NewVideoGenerationService constructs the production service, wired to the
// real DashScope client.
func NewVideoGenerationService(settings SettingReader, tasks repository.TaskRepository) *VideoGenerationService {
	return NewVideoGenerationServiceWithFactory(settings, tasks, NewDashScopeVideoProviderFactory())
}

// NewVideoGenerationServiceWithFactory allows tests to inject a fake
// VideoProviderFactory — no real network, no real database (tasks is an
// interface too).
func NewVideoGenerationServiceWithFactory(settings SettingReader, tasks repository.TaskRepository, factory VideoProviderFactory) *VideoGenerationService {
	return &VideoGenerationService{settings: settings, tasks: tasks, factory: factory}
}

// CreateVideoTask validates against the catalog, fetches credentials, calls
// upstream to create the async task, and persists a PENDING row. It returns
// the persisted *generationmodel.Task — callers must use task.ID (the local
// row id), never task.UpstreamTaskID, to address this task in any later
// GetTask/ListTasks call.
func (s *VideoGenerationService) CreateVideoTask(ctx context.Context, userID int64, req CreateVideoTaskRequest) (*generationmodel.Task, error) {
	model, ok := catalog.ByID(req.Model)
	if !ok {
		return nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 未知的模型 %q", req.Model))
	}

	mode, media, err := buildVideoMedia(model, req)
	if err != nil {
		return nil, err
	}

	params, err := normalizeVideoParams(mode, req.Params)
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
	endpoint, err := s.settings.GetDecrypted(ctx, settingmodel.KeyEndpoint)
	if err != nil {
		return nil, err
	}
	workspaceID, err := s.settings.GetDecrypted(ctx, settingmodel.KeyWorkspaceID)
	if err != nil {
		return nil, err
	}

	payload := buildVideoPayload(model, mode, req, media, params)

	p := s.factory(apiKey, region, workspaceID, endpoint)
	upstreamID, callErr := p.CreateVideoTask(ctx, provider.VideoRequest{Payload: payload})

	task := &generationmodel.Task{
		UserID:    userID,
		Mode:      mode,
		Model:     req.Model,
		Prompt:    req.Prompt,
		Params:    videoParamsToStorageMap(params),
		InputURLs: mediaURLs(media),
	}

	if callErr != nil {
		code, msg := errorCodeAndMessage(callErr)
		task.Status = generationmodel.StatusFailed
		task.ErrorCode = &code
		task.ErrorMessage = &msg
		if createErr := s.tasks.Create(ctx, task); createErr != nil {
			// 落库失败不能掩盖真正的上游失败原因——调用方需要看到的是
			// callErr（比如"上游拒绝"），不是这个次生的数据库错误。
			slog.Error("generation_video: 落库 FAILED 任务失败", "userID", userID, "model", req.Model, "err", createErr)
		}
		return nil, callErr
	}

	task.Status = generationmodel.StatusPending
	task.UpstreamTaskID = &upstreamID
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

// buildVideoMedia dispatches on the model's catalog capability (each video
// model in the catalog has exactly one of t2v/i2v/r2v — there is no overlap
// to disambiguate, unlike image models which can be both t2i and edit) and
// returns the resolved TaskMode plus the ordered media array for the
// upstream payload. It validates required-field presence for the resolved
// mode/task-type combination but does not touch VideoParams — that's
// validateVideoParams's job, kept separate because it applies uniformly
// across all three modes.
func buildVideoMedia(model catalog.Model, req CreateVideoTaskRequest) (generationmodel.TaskMode, []map[string]any, error) {
	switch {
	case model.HasCapability(catalog.CapabilityR2V):
		return buildR2VRequest(model, req)
	case model.HasCapability(catalog.CapabilityI2V):
		return buildI2VRequest(model, req)
	case model.HasCapability(catalog.CapabilityT2V):
		if req.Prompt == "" {
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: t2v 需要 prompt"))
		}
		return generationmodel.TaskModeT2V, nil, nil
	default:
		return "", nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不具备任何视频生成能力", model.ID))
	}
}

// buildR2VRequest ports r2v.js:138-186. isWan mirrors r2v.js's
// `isWanR2v()` (a literal model-id compare); in the catalog the wan r2v
// model is exactly the one with a non-empty Regions whitelist, so that's
// the signal used here instead of hardcoding the id a second time.
func buildR2VRequest(model catalog.Model, req CreateVideoTaskRequest) (generationmodel.TaskMode, []map[string]any, error) {
	if req.Prompt == "" {
		return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: r2v 需要 prompt"))
	}

	isWan := len(model.Regions) > 0
	if !isWan {
		if len(req.Videos) > 0 {
			return "", nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不支持参考视频", model.ID))
		}
		for _, img := range req.Images {
			if img.ReferenceVoice != "" {
				return "", nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 不支持 reference_voice", model.ID))
			}
		}
	}

	total := len(req.Images) + len(req.Videos)
	if total == 0 {
		return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: r2v 需要至少一张参考图或一个参考视频"))
	}
	if model.MaxImages > 0 && total > model.MaxImages {
		return "", nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 参考媒体数量 %d 超过模型 %q 的上限 %d", total, model.ID, model.MaxImages))
	}

	return generationmodel.TaskModeR2V, buildR2VMedia(req.Images, req.Videos), nil
}

// buildI2VRequest ports i2v.js:212-263. Non-wan (happyhorse-1.1-i2v) always
// behaves as first_frame regardless of the caller-supplied TaskType — the
// old UI never even exposes task-type tabs for that model (i2v.js only
// switches media assembly logic on `wan`, not on `task`, for the non-wan
// branch; see i2v.js:253-263).
func buildI2VRequest(model catalog.Model, req CreateVideoTaskRequest) (generationmodel.TaskMode, []map[string]any, error) {
	isWan := len(model.Regions) > 0

	if !isWan {
		if req.FirstFrame == "" {
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: i2v 需要首帧图片"))
		}
		if req.LastFrame != "" || req.FirstClip != "" || req.DrivingAudio != "" {
			return "", nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 模型 %q 只支持首帧图片", model.ID))
		}
		return generationmodel.TaskModeI2V, buildI2VMedia(I2VTaskFirstFrame, req.FirstFrame, "", "", ""), nil
	}

	switch req.TaskType {
	case I2VTaskFirstFrame:
		if req.FirstFrame == "" {
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_frame 任务需要首帧图片"))
		}
		if req.LastFrame != "" || req.FirstClip != "" {
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_frame 任务不接受尾帧/续接片段"))
		}
	case I2VTaskFirstLast:
		if req.FirstFrame == "" || req.LastFrame == "" {
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_last 任务需要首帧与尾帧图片"))
		}
		if req.FirstClip != "" {
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: first_last 任务不接受续接片段"))
		}
	case I2VTaskContinue:
		if req.FirstClip == "" {
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: continue 任务需要续接片段"))
		}
		if req.FirstFrame != "" {
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: continue 任务不接受首帧图片"))
		}
		if req.DrivingAudio != "" {
			// i2v.js:236 只在 first_frame/first_last 任务上附加
			// driving_audio；continue 任务的 UI 从不展示这个输入框。
			return "", nil, apperr.ErrValidation.Wrap(errors.New("generation_video: continue 任务不接受 driving_audio"))
		}
	default:
		return "", nil, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 未知的 i2v 任务类型 %q", req.TaskType))
	}

	return generationmodel.TaskModeI2V, buildI2VMedia(req.TaskType, req.FirstFrame, req.LastFrame, req.FirstClip, req.DrivingAudio), nil
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

// videoResolutions / videoDefaultResolution, videoDurationMin/Max /
// videoDefaultDuration, and videoRatios / videoDefaultRatio are the allowed
// values for resolution/duration/ratio, ported from the <select>/<input>
// controls in public/index.html (resolution-r2v/i2v/t2v, duration-*,
// ratio-r2v/t2v — see index.html:91-110,240-247,329-348).
//
// These live here as package-level constants, not as fields on
// catalog.Model, because they are not model-specific: every video model in
// the catalog (happyhorse and wan alike) takes the same resolution set, the
// same duration range, and — for the modes that accept a ratio at all — the
// same ratio set. r2v.js and t2v.js's <select id="ratio-*"> list the same
// seven values in a different on-screen order, but the allowed *set* is
// identical, so one shared slice covers both. Bolting mode-invariant
// constants onto every catalog.Model entry would just be denormalization;
// catalog.go stays untouched, matching Task 12's file list.
var videoResolutions = []string{"720P", "1080P"}

const videoDefaultResolution = "720P"

const (
	videoDurationMin     = 3
	videoDurationMax     = 15
	videoDefaultDuration = 5
)

// videoRatios is the shared allowed set for r2v/t2v's ratio dropdown
// (index.html:98-106 for r2v, :336-344 for t2v — same seven values, listed
// in a different order in the HTML, which is why this is a set, not
// something that needs to preserve either page's on-screen ordering).
var videoRatios = []string{"16:9", "9:16", "3:4", "4:3", "4:5", "5:4", "1:1"}

const videoDefaultRatio = "16:9"

// normalizeVideoParams applies task.js:304-315's defaulting rules and
// rejects anything outside the allowed value sets. It must run before
// validateOptionalVideoParams/buildVideoPayload — both downstream consumers
// assume Resolution/Duration are already valid and defaulted, and that
// Ratio is empty if and only if mode is i2v.
//
// resolution/duration/watermark are mandatory-with-a-default: the old UI's
// controls always have a value, so a caller that omits them gets the same
// default the old UI would have produced (720P / 5s), not an error.
// Watermark has no validation branch at all — every bool value is valid,
// so "not set" (false) and "explicitly false" are indistinguishable and
// that's fine, matching collectWatermarkParams' unconditional
// `{watermark: enabled}`.
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
// first frame and never gets a ratio parameter — is nonetheless correct,
// so this function makes it deliberate: i2v requests that set Ratio at all
// are rejected outright (silently dropping a caller-supplied parameter is
// how you get a bug report when someone "fixes" the missing UI field six
// months from now), and r2v/t2v default an empty Ratio to "16:9" instead of
// leaving it unset — mirroring the <select> always having a selected
// option, which is the actual reason r2v/t2v always send one.
func normalizeVideoParams(mode generationmodel.TaskMode, p VideoParams) (VideoParams, error) {
	out := p

	switch {
	case out.Resolution == "":
		out.Resolution = videoDefaultResolution
	case !containsString(videoResolutions, out.Resolution):
		return VideoParams{}, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 不支持的 resolution %q", out.Resolution))
	}

	switch {
	case out.Duration == 0:
		out.Duration = videoDefaultDuration
	case out.Duration < videoDurationMin || out.Duration > videoDurationMax:
		return VideoParams{}, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: duration=%d 超出允许范围 [%d,%d]", out.Duration, videoDurationMin, videoDurationMax))
	}

	if mode == generationmodel.TaskModeI2V {
		if out.Ratio != "" {
			return VideoParams{}, apperr.ErrValidation.Wrap(errors.New("generation_video: i2v 不接受 ratio，宽高比由首帧决定"))
		}
		return out, nil
	}

	switch {
	case out.Ratio == "":
		out.Ratio = videoDefaultRatio
	case !containsString(videoRatios, out.Ratio):
		return VideoParams{}, apperr.ErrValidation.Wrap(fmt.Errorf("generation_video: 不支持的 ratio %q", out.Ratio))
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
	return m
}
