package service_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider/dashscope"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

func newVideoService(t *testing.T, settings *fakeOptimizeSettings, factory *recordingVideoFactory) (*service.VideoGenerationService, *fakeTaskRepo) {
	t.Helper()
	repo := newFakeTaskRepo()
	svc := service.NewVideoGenerationServiceWithFactory(settings, repo, factory.Factory())
	return svc, repo
}

func mediaOf(t *testing.T, factory *recordingVideoFactory) []map[string]any {
	t.Helper()
	input, ok := factory.lastReq.Payload["input"].(map[string]any)
	require.True(t, ok, "payload 必须有 input 字段: %#v", factory.lastReq.Payload)
	media, ok := input["media"].([]map[string]any)
	require.True(t, ok, "input.media 必须是 []map[string]any: %#v", input["media"])
	return media
}

func parametersOf(t *testing.T, factory *recordingVideoFactory) map[string]any {
	t.Helper()
	parameters, ok := factory.lastReq.Payload["parameters"].(map[string]any)
	require.True(t, ok, "payload 必须有 parameters 字段: %#v", factory.lastReq.Payload)
	return parameters
}

// ── Create 返回本地 id，不是上游 id ─────────────────────────────────

func TestCreateVideoTask_ReturnsLocalID_DistinctFromUpstreamID(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "dashscope-upstream-task-abc123"}
	svc, repo := newVideoService(t, settings, factory)

	task, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "a cat running",
	})
	require.NoError(t, err)

	assert.NotZero(t, task.ID, "必须真的落库拿到本地自增 ID")
	require.NotNil(t, task.UpstreamTaskID)
	assert.Equal(t, "dashscope-upstream-task-abc123", *task.UpstreamTaskID)

	// 本地 ID 是数据库自增 int64，上游 ID 是 DashScope 返回的字符串——
	// 两者的取值空间完全不同，混用不可能"碰巧"通过断言。
	assert.NotEqual(t, task.ID, *task.UpstreamTaskID)
	assert.Equal(t, generationmodel.StatusPending, task.Status)

	require.Len(t, repo.all(), 1)
	assert.Equal(t, task.ID, repo.all()[0].ID)
}

// ── wan2.7 视频模型的 region 白名单 ──────────────────────────────────

func TestCreateVideoTask_Wan27_USEast1_Rejected_CNBeijing_Accepted(t *testing.T) {
	rejectSettings := dashscopeSettings(map[settingmodel.Key]string{settingmodel.KeyRegion: "us-east-1"})
	rejectFactory := &recordingVideoFactory{taskID: "t-1"}
	svc, repo := newVideoService(t, rejectSettings, rejectFactory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "wan2.7-r2v",
		Prompt: "x",
		Images: []service.R2VMediaImage{{URL: "https://example.com/1.png"}},
	})
	require.Error(t, err, "us-east-1 不在 wan2.7 的 region 白名单里")
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, rejectFactory.calls, "region 被拒时不应该打任何上游请求")
	assert.Empty(t, repo.all(), "region 被拒时不落库")

	okSettings := dashscopeSettings(map[settingmodel.Key]string{settingmodel.KeyRegion: "cn-beijing"})
	okFactory := &recordingVideoFactory{taskID: "t-2"}
	svc2, _ := newVideoService(t, okSettings, okFactory)

	_, err = svc2.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "wan2.7-r2v",
		Prompt: "x",
		Images: []service.R2VMediaImage{{URL: "https://example.com/1.png"}},
	})
	require.NoError(t, err, "cn-beijing 在白名单内应该被接受")
	assert.Equal(t, 1, okFactory.calls)
}

func TestCreateVideoTask_Wan27I2V_USEast1_Rejected(t *testing.T) {
	settings := dashscopeSettings(map[settingmodel.Key]string{settingmodel.KeyRegion: "us-east-1"})
	factory := &recordingVideoFactory{taskID: "t-1"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:      "wan2.7-i2v-2026-04-25",
		Prompt:     "x",
		TaskType:   service.I2VTaskFirstFrame,
		FirstFrame: "https://example.com/first.png",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}

// ── 归属隔离 ────────────────────────────────────────────────────────

func TestGetTask_OtherUsersTask_NotFound_NotForbidden(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-owner"}
	svc, _ := newVideoService(t, settings, factory)

	owned, err := svc.CreateVideoTask(context.Background(), 111, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "owned by user 111",
	})
	require.NoError(t, err)

	_, err = svc.GetTask(context.Background(), 222, owned.ID)
	require.Error(t, err)
	// 必须是 NOT_FOUND，不是 FORBIDDEN——403 会向调用方泄露"这个 id 确实
	// 存在，只是不属于你"，本身就是一个存在性 oracle。
	assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))
	assert.False(t, errors.Is(err, apperr.ErrForbidden))

	// 属主自己能看到。
	got, err := svc.GetTask(context.Background(), 111, owned.ID)
	require.NoError(t, err)
	assert.Equal(t, owned.ID, got.ID)
}

// ── 删除 ────────────────────────────────────────────────────────────

func TestDeleteTask_OtherUsersTask_NotFound_NotForbidden_LeavesRowIntact(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-owner"}
	svc, repo := newVideoService(t, settings, factory)

	owned, err := svc.CreateVideoTask(context.Background(), 111, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "owned by user 111",
	})
	require.NoError(t, err)

	err = svc.DeleteTask(context.Background(), 222, owned.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))
	assert.False(t, errors.Is(err, apperr.ErrForbidden))

	// 失败的删除不能有任何副作用——属主自己应该还能看到这条任务。
	got, err := svc.GetTask(context.Background(), 111, owned.ID)
	require.NoError(t, err)
	assert.Equal(t, owned.ID, got.ID)
	require.Len(t, repo.all(), 1)
}

func TestDeleteTask_OwnTask_RemovesIt_SecondDeleteNotFound(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-owner"}
	svc, _ := newVideoService(t, settings, factory)

	owned, err := svc.CreateVideoTask(context.Background(), 111, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "owned by user 111",
	})
	require.NoError(t, err)

	require.NoError(t, svc.DeleteTask(context.Background(), 111, owned.ID))

	_, err = svc.GetTask(context.Background(), 111, owned.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))

	err = svc.DeleteTask(context.Background(), 111, owned.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrTaskNotFound), "已经删过的任务再删一次应报 NOT_FOUND")
}

func TestDeleteAllTasks_OnlyRemovesCallersOwnRows_ReturnsCount(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-1"}
	svc, repo := newVideoService(t, settings, factory)

	for i := 0; i < 3; i++ {
		_, err := svc.CreateVideoTask(context.Background(), 111, service.CreateVideoTaskRequest{
			Model: "happyhorse-1.1-t2v", Prompt: "x",
		})
		require.NoError(t, err)
	}
	other, err := svc.CreateVideoTask(context.Background(), 222, service.CreateVideoTaskRequest{
		Model: "happyhorse-1.1-t2v", Prompt: "not mine",
	})
	require.NoError(t, err)

	deleted, err := svc.DeleteAllTasks(context.Background(), 111)
	require.NoError(t, err)
	assert.Equal(t, int64(3), deleted)

	items, total, err := svc.ListTasks(context.Background(), 111, generationmodel.ListQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, items)

	// 别的用户的任务不受影响。
	got, err := svc.GetTask(context.Background(), 222, other.ID)
	require.NoError(t, err)
	assert.Equal(t, other.ID, got.ID)
	require.Len(t, repo.all(), 1)
}

func TestDeleteAllTasks_NoTasks_ReturnsZeroNoError(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-1"}
	svc, _ := newVideoService(t, settings, factory)

	deleted, err := svc.DeleteAllTasks(context.Background(), 111)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

// TestDeleteTask_InFlightTask_StillAllowed pins the deliberate decision on
// service.VideoGenerationService.DeleteTask's doc: deletion is not gated on
// task status, so a PENDING task (possibly mid-poll in internal/worker)
// deletes exactly like any terminal-state task.
func TestDeleteTask_InFlightTask_StillAllowed(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-1"}
	svc, _ := newVideoService(t, settings, factory)

	task, err := svc.CreateVideoTask(context.Background(), 111, service.CreateVideoTaskRequest{
		Model: "happyhorse-1.1-t2v", Prompt: "x",
	})
	require.NoError(t, err)
	require.Equal(t, generationmodel.StatusPending, task.Status, "CreateVideoTask 落库应处于 PENDING，才是这个测试想覆盖的在飞状态")

	require.NoError(t, svc.DeleteTask(context.Background(), 111, task.ID), "PENDING 任务也应允许删除")

	_, err = svc.GetTask(context.Background(), 111, task.ID)
	assert.True(t, errors.Is(err, apperr.ErrTaskNotFound))
}

// ── media 顺序：r2v ─────────────────────────────────────────────────

func TestCreateVideoTask_R2V_MediaOrder_ImagesFirstThenVideos(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-r2v"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "wan2.7-r2v",
		Prompt: "x",
		Images: []service.R2VMediaImage{
			{URL: "img1", ReferenceVoice: "voice1"},
			{URL: "img2"},
		},
		Videos: []service.R2VMediaVideo{
			{URL: "vid1", ReferenceVoice: "voice-v1"},
			{URL: "vid2"},
		},
	})
	require.NoError(t, err)

	want := []map[string]any{
		{"type": "reference_image", "url": "img1", "reference_voice": "voice1"},
		{"type": "reference_image", "url": "img2"},
		{"type": "reference_video", "url": "vid1", "reference_voice": "voice-v1"},
		{"type": "reference_video", "url": "vid2"},
	}
	assert.Equal(t, want, mediaOf(t, factory))
}

func TestCreateVideoTask_R2V_HappyHorse_RejectsReferenceVoiceAndVideos(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-r2v"}
	svc, _ := newVideoService(t, settings, factory)

	// happyhorse-1.1-r2v 不是 wan，reference_voice 不该被接受，即便是通过
	// curl 直接打接口绕过前端 UI。
	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-r2v",
		Prompt: "x",
		Images: []service.R2VMediaImage{{URL: "img1", ReferenceVoice: "voice1"}},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)

	_, err = svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-r2v",
		Prompt: "x",
		Videos: []service.R2VMediaVideo{{URL: "vid1"}},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}

// ── media 顺序：i2v 三种任务类型 ─────────────────────────────────────

func TestCreateVideoTask_I2V_FirstFrame_MediaOrder(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-i2v"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:        "wan2.7-i2v-2026-04-25",
		Prompt:       "x",
		TaskType:     service.I2VTaskFirstFrame,
		FirstFrame:   "first.png",
		DrivingAudio: "audio.mp3",
	})
	require.NoError(t, err)

	want := []map[string]any{
		{"type": "first_frame", "url": "first.png"},
		{"type": "driving_audio", "url": "audio.mp3"},
	}
	assert.Equal(t, want, mediaOf(t, factory))
}

func TestCreateVideoTask_I2V_FirstLast_MediaOrder(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-i2v"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:        "wan2.7-i2v-2026-04-25",
		Prompt:       "x",
		TaskType:     service.I2VTaskFirstLast,
		FirstFrame:   "first.png",
		LastFrame:    "last.png",
		DrivingAudio: "audio.mp3",
	})
	require.NoError(t, err)

	want := []map[string]any{
		{"type": "first_frame", "url": "first.png"},
		{"type": "last_frame", "url": "last.png"},
		{"type": "driving_audio", "url": "audio.mp3"},
	}
	assert.Equal(t, want, mediaOf(t, factory))
}

func TestCreateVideoTask_I2V_Continue_MediaOrder_WithOptionalLastFrame(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-i2v"}
	svc, _ := newVideoService(t, settings, factory)

	// continue 任务里 last_frame 是可选的尾随约束——driving_audio 在这个
	// 任务类型上不适用（即便传了也不该出现在 media 里；这里干脆不传）。
	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:     "wan2.7-i2v-2026-04-25",
		Prompt:    "x",
		TaskType:  service.I2VTaskContinue,
		FirstClip: "clip.mp4",
		LastFrame: "constraint.png",
	})
	require.NoError(t, err)

	want := []map[string]any{
		{"type": "first_clip", "url": "clip.mp4"},
		{"type": "last_frame", "url": "constraint.png"},
	}
	assert.Equal(t, want, mediaOf(t, factory))
}

func TestCreateVideoTask_I2V_Continue_WithoutLastFrame_OmitsSlot(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-i2v"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:     "wan2.7-i2v-2026-04-25",
		Prompt:    "x",
		TaskType:  service.I2VTaskContinue,
		FirstClip: "clip.mp4",
	})
	require.NoError(t, err)

	want := []map[string]any{
		{"type": "first_clip", "url": "clip.mp4"},
	}
	assert.Equal(t, want, mediaOf(t, factory))
}

func TestCreateVideoTask_I2V_Continue_RejectsDrivingAudio(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-i2v"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:        "wan2.7-i2v-2026-04-25",
		Prompt:       "x",
		TaskType:     service.I2VTaskContinue,
		FirstClip:    "clip.mp4",
		DrivingAudio: "audio.mp3",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}

func TestCreateVideoTask_I2V_HappyHorse_AlwaysFirstFrame(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-i2v"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:      "happyhorse-1.1-i2v",
		Prompt:     "x",
		FirstFrame: "first.png",
	})
	require.NoError(t, err)

	want := []map[string]any{
		{"type": "first_frame", "url": "first.png"},
	}
	assert.Equal(t, want, mediaOf(t, factory))
}

// ── 上游失败仍落 FAILED 行 ────────────────────────────────────────────

func TestCreateVideoTask_UpstreamFailure_PersistsFailedRow(t *testing.T) {
	settings := dashscopeSettings(nil)
	upstreamErr := apperr.ErrValidation.Wrap(errors.New("boom"))
	factory := &recordingVideoFactory{err: upstreamErr}
	svc, repo := newVideoService(t, settings, factory)

	task, err := svc.CreateVideoTask(context.Background(), 5, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "x",
	})
	require.Error(t, err)
	assert.Nil(t, task)

	rows := repo.all()
	require.Len(t, rows, 1)
	assert.Equal(t, generationmodel.StatusFailed, rows[0].Status)
	require.NotNil(t, rows[0].ErrorCode)
}

// TestCreateVideoTask_UpstreamAuthFailure_NormalizedTo502NotBare401 mirrors
// generation_image_test.go's equivalent case for the video path: DashScope
// answering with 401 for an invalid API key must not surface as our own
// 401, or web/src/api/client.ts's interceptor logs the user out.
func TestCreateVideoTask_UpstreamAuthFailure_NormalizedTo502NotBare401(t *testing.T) {
	settings := dashscopeSettings(nil)
	upstreamErr := apperr.New(dashscope.CodeUpstreamHTTP, http.StatusUnauthorized).
		Wrap(errors.New("dashscope: invalid api key"))
	factory := &recordingVideoFactory{err: upstreamErr}
	svc, repo := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 5, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "x",
	})

	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUpstreamAuthFailed.Code(), appErr.Code())
	assert.Equal(t, http.StatusBadGateway, appErr.HTTPStatus(), "上游 401 绝不能变成我们自己的 401")
	assert.NotEqual(t, http.StatusUnauthorized, appErr.HTTPStatus())

	require.NotNil(t, appErr.Internal())
	assert.Contains(t, appErr.Internal().Error(), "invalid api key")

	rows := repo.all()
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].ErrorCode)
	assert.Equal(t, apperr.ErrUpstreamAuthFailed.Code(), *rows[0].ErrorCode)
}

// TestCreateVideoTask_UpstreamRateLimit_NormalizedTo429WithDedicatedCode
// mirrors the image path's 429 case.
func TestCreateVideoTask_UpstreamRateLimit_NormalizedTo429WithDedicatedCode(t *testing.T) {
	settings := dashscopeSettings(nil)
	upstreamErr := apperr.New(dashscope.CodeUpstreamHTTP, http.StatusTooManyRequests).
		Wrap(errors.New("dashscope: throttling"))
	factory := &recordingVideoFactory{err: upstreamErr}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 5, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "x",
	})

	require.Error(t, err)
	var appErr *apperr.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.ErrUpstreamRateLimited.Code(), appErr.Code())
	assert.Equal(t, http.StatusTooManyRequests, appErr.HTTPStatus())
}

// ── resolution/duration/watermark/ratio ──────────────────────────────

// Every video mode must emit resolution/duration/watermark unconditionally
// — task.js:304-315 never omits them for any of r2v/i2v/t2v.
func TestCreateVideoTask_AllModes_AlwaysEmitResolutionDurationWatermark(t *testing.T) {
	cases := []struct {
		name string
		req  service.CreateVideoTaskRequest
	}{
		{
			name: "t2v",
			req: service.CreateVideoTaskRequest{
				Model:  "happyhorse-1.1-t2v",
				Prompt: "x",
			},
		},
		{
			name: "r2v",
			req: service.CreateVideoTaskRequest{
				Model:  "wan2.7-r2v",
				Prompt: "x",
				Images: []service.R2VMediaImage{{URL: "img1"}},
			},
		},
		{
			name: "i2v",
			req: service.CreateVideoTaskRequest{
				Model:      "wan2.7-i2v-2026-04-25",
				Prompt:     "x",
				TaskType:   service.I2VTaskFirstFrame,
				FirstFrame: "first.png",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := dashscopeSettings(nil)
			factory := &recordingVideoFactory{taskID: "t-1"}
			svc, _ := newVideoService(t, settings, factory)

			_, err := svc.CreateVideoTask(context.Background(), 1, tc.req)
			require.NoError(t, err)

			params := parametersOf(t, factory)
			assert.Equal(t, "720P", params["resolution"], "未指定时必须默认 720P")
			assert.Equal(t, 5, params["duration"], "未指定时必须默认 5 秒")
			assert.Equal(t, false, params["watermark"], "watermark 必须始终发送，即便是 false")
		})
	}
}

// watermark:false 和 seed:0 都是有意义的显式值，不能被判空逻辑丢弃。
func TestCreateVideoTask_WatermarkFalse_And_SeedZero_AreSent_NotDropped(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-1"}
	svc, _ := newVideoService(t, settings, factory)

	zero := int64(0)
	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "x",
		Params: service.VideoParams{Watermark: false, Seed: &zero},
	})
	require.NoError(t, err)

	params := parametersOf(t, factory)
	require.Contains(t, params, "watermark")
	assert.Equal(t, false, params["watermark"])
	require.Contains(t, params, "seed")
	assert.Equal(t, int64(0), params["seed"])
}

// r2v 与 t2v 必须发送 ratio；i2v 必须完全不发送（宽高比由首帧决定）。
func TestCreateVideoTask_Ratio_SentForR2VAndT2V_OmittedForI2V(t *testing.T) {
	settings := dashscopeSettings(nil)

	r2vFactory := &recordingVideoFactory{taskID: "t-r2v"}
	r2vSvc, _ := newVideoService(t, settings, r2vFactory)
	_, err := r2vSvc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "wan2.7-r2v",
		Prompt: "x",
		Images: []service.R2VMediaImage{{URL: "img1"}},
	})
	require.NoError(t, err)
	r2vParams := parametersOf(t, r2vFactory)
	assert.Equal(t, "16:9", r2vParams["ratio"], "r2v 未指定 ratio 时必须默认 16:9")

	t2vFactory := &recordingVideoFactory{taskID: "t-t2v"}
	t2vSvc, _ := newVideoService(t, settings, t2vFactory)
	_, err = t2vSvc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "x",
		Params: service.VideoParams{Ratio: "9:16"},
	})
	require.NoError(t, err)
	t2vParams := parametersOf(t, t2vFactory)
	assert.Equal(t, "9:16", t2vParams["ratio"])

	i2vFactory := &recordingVideoFactory{taskID: "t-i2v"}
	i2vSvc, _ := newVideoService(t, settings, i2vFactory)
	_, err = i2vSvc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:      "wan2.7-i2v-2026-04-25",
		Prompt:     "x",
		TaskType:   service.I2VTaskFirstFrame,
		FirstFrame: "first.png",
	})
	require.NoError(t, err)
	i2vParams := parametersOf(t, i2vFactory)
	assert.NotContains(t, i2vParams, "ratio", "i2v 的宽高比由首帧决定，绝不应该出现在 parameters 里")
}

// 显式在 i2v 请求上设置 ratio 必须被拒绝——静默丢弃调用方明确设置的参数，
// 是六个月后一个诡异 bug 报告的来源。
func TestCreateVideoTask_I2V_ExplicitRatio_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-i2v"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:      "wan2.7-i2v-2026-04-25",
		Prompt:     "x",
		TaskType:   service.I2VTaskFirstFrame,
		FirstFrame: "first.png",
		Params:     service.VideoParams{Ratio: "16:9"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls, "校验失败不应该打任何上游请求")
}

// duration 超出 [3,15] 拒绝；边界值 3 和 15 接受。
func TestCreateVideoTask_Duration_OutOfRangeRejected_BoundariesAccepted(t *testing.T) {
	settings := dashscopeSettings(nil)

	for _, d := range []int{2, 16} {
		factory := &recordingVideoFactory{taskID: "t-1"}
		svc, _ := newVideoService(t, settings, factory)
		_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
			Model:  "happyhorse-1.1-t2v",
			Prompt: "x",
			Params: service.VideoParams{Duration: d},
		})
		require.Error(t, err, "duration=%d 应该被拒绝", d)
		assert.True(t, errors.Is(err, apperr.ErrValidation))
		assert.Equal(t, 0, factory.calls)
	}

	for _, d := range []int{3, 15} {
		factory := &recordingVideoFactory{taskID: "t-1"}
		svc, _ := newVideoService(t, settings, factory)
		_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
			Model:  "happyhorse-1.1-t2v",
			Prompt: "x",
			Params: service.VideoParams{Duration: d},
		})
		require.NoError(t, err, "duration=%d 应该被接受", d)
		params := parametersOf(t, factory)
		assert.Equal(t, d, params["duration"])
	}
}

// 非法 resolution 拒绝。
func TestCreateVideoTask_InvalidResolution_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-1"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "x",
		Params: service.VideoParams{Resolution: "4K"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}

// r2v 上非法 ratio 拒绝。
func TestCreateVideoTask_R2V_InvalidRatio_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t-1"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "wan2.7-r2v",
		Prompt: "x",
		Images: []service.R2VMediaImage{{URL: "img1"}},
		Params: service.VideoParams{Ratio: "21:9"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperr.ErrValidation))
	assert.Equal(t, 0, factory.calls)
}
