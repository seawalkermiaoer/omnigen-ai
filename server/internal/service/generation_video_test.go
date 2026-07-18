package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
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
