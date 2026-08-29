// Package service_test — wan3.0-specific video generation tests.
//
// These live in their own file rather than mixed into
// generation_video_test.go because they exercise a different contract: every
// test there is about a model with exactly one video capability, where the
// mode is inferred and the media rules are wan2.7's. wan3.0 inverts both —
// one model id, five modes, an explicit Mode field, and media limits counted
// per type. Keeping them apart makes it obvious which set of rules a given
// assertion is pinning down.
//
// 事实来源：万相3.0视频生成API参考
// https://help.aliyun.com/zh/model-studio/wan3-video-generation-api-reference
package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	settingmodel "github.com/chenhao/omnigen-ai/server/internal/model/setting"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

const wan30 = "wan3.0-video"

// ── 模式解析 ────────────────────────────────────────────────────────

// wan3.0 有五种视频能力，模式无法从 model id 推出来，必须显式给。
// 这里断言的是"报错"而不是"猜一个"：猜测会让"忘了传首帧"变成一次
// 计费的文生视频。
func TestWan30_MissingMode_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  wan30,
		Prompt: "a cat",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
	assert.Nil(t, factory.lastReq.Payload, "校验失败不该调上游")
}

// 反过来：单能力模型仍然可以不传 mode——这条保证了 wan3.0 的引入没有
// 改变任何既有调用方的契约。
func TestWan30_SingleCapabilityModel_ModeStillOptional(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	task, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "a cat",
	})
	require.NoError(t, err)
	assert.Equal(t, generationmodel.TaskModeT2V, task.Mode)
}

// 给单能力模型传一个它没有的 mode 也要报错，而不是被忽略后按它唯一
// 的能力执行——那样调用方永远发现不了自己发错了请求。
func TestWan30_ModeNotSupportedByModel_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "happyhorse-1.1-t2v",
		Prompt: "a cat",
		Mode:   generationmodel.TaskModeR2V,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// ── 五种模式各自的 media 形状 ────────────────────────────────────────

func TestWan30_T2V_NoMediaKey(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	task, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  wan30,
		Mode:   generationmodel.TaskModeT2V,
		Prompt: "一只小猫在月光下的屋顶上奔跑",
	})
	require.NoError(t, err)
	assert.Equal(t, generationmodel.TaskModeT2V, task.Mode)

	input := factory.lastReq.Payload["input"].(map[string]any)
	_, hasMedia := input["media"]
	assert.False(t, hasMedia, "文生视频不该出现 media 字段: %#v", input)
	assert.Equal(t, "一只小猫在月光下的屋顶上奔跑", input["prompt"])
}

func TestWan30_I2V_FirstFrameOnly(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:      wan30,
		Mode:       generationmodel.TaskModeI2V,
		Prompt:     "x",
		TaskType:   service.I2VTaskFirstFrame,
		FirstFrame: "https://example.com/first.png",
	})
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{
		{"type": "first_frame", "url": "https://example.com/first.png"},
	}, mediaOf(t, factory))
}

func TestWan30_I2V_FirstLast(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:      wan30,
		Mode:       generationmodel.TaskModeI2V,
		Prompt:     "x",
		TaskType:   service.I2VTaskFirstLast,
		FirstFrame: "https://example.com/first.png",
		LastFrame:  "https://example.com/last.jpg",
	})
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{
		{"type": "first_frame", "url": "https://example.com/first.png"},
		{"type": "last_frame", "url": "https://example.com/last.jpg"},
	}, mediaOf(t, factory))
}

// first_last 却没给尾帧是自相矛盾的请求：必须报错，不能降级成 first_frame
// 悄悄生成一个用户没要的东西。
func TestWan30_I2V_FirstLastWithoutLastFrame_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:      wan30,
		Mode:       generationmodel.TaskModeI2V,
		TaskType:   service.I2VTaskFirstLast,
		FirstFrame: "https://example.com/first.png",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// wan3.0 没有 first_clip 这种媒体类型，续接任务整体不存在——报错文案要把
// 替代路径（参考视频 + 延长意图 prompt）说出来，所以这里连消息一起断言。
func TestWan30_I2V_Continue_RejectedWithMigrationHint(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:     wan30,
		Mode:      generationmodel.TaskModeI2V,
		TaskType:  service.I2VTaskContinue,
		FirstClip: "https://example.com/clip.mp4",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
	assert.Contains(t, err.Error(), "参考生视频")
}

func TestWan30_I2V_DrivingAudio_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:        wan30,
		Mode:         generationmodel.TaskModeI2V,
		TaskType:     service.I2VTaskFirstFrame,
		FirstFrame:   "https://example.com/first.png",
		DrivingAudio: "https://example.com/voice.mp3",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

func TestWan30_R2V_MediaOrder_ImagesThenVideosThenAudios(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  wan30,
		Mode:   generationmodel.TaskModeR2V,
		Prompt: "视频1抱着图3",
		Images: []service.R2VMediaImage{{URL: "img1"}, {URL: "img2"}},
		Videos: []service.R2VMediaVideo{{URL: "vid1"}},
		Audios: []service.R2VMediaAudio{{URL: "aud1"}},
	})
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{
		{"type": "reference_image", "url": "img1"},
		{"type": "reference_image", "url": "img2"},
		{"type": "reference_video", "url": "vid1"},
		{"type": "reference_audio", "url": "aud1"},
	}, mediaOf(t, factory))
}

// wan3.0 的 media 条目只有 type 与 url——reference_voice 是 wan2.7 的概念，
// 传了要报错而不是被丢掉。
func TestWan30_R2V_ReferenceVoice_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  wan30,
		Mode:   generationmodel.TaskModeR2V,
		Prompt: "x",
		Images: []service.R2VMediaImage{{URL: "img1", ReferenceVoice: "voice1"}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// 上限是按类型分别算的（10/5/5），不是 wan2.7 那种合计 5——12 张图必须
// 被拒，而 10 图 + 5 视频 + 5 音频（合计 20）必须通过。
func TestWan30_R2V_PerTypeLimits(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	tooManyImages := make([]service.R2VMediaImage, 11)
	for i := range tooManyImages {
		tooManyImages[i] = service.R2VMediaImage{URL: "img"}
	}
	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeR2V, Prompt: "x", Images: tooManyImages,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)

	atLimit := struct {
		images []service.R2VMediaImage
		videos []service.R2VMediaVideo
		audios []service.R2VMediaAudio
	}{
		images: make([]service.R2VMediaImage, 10),
		videos: make([]service.R2VMediaVideo, 5),
		audios: make([]service.R2VMediaAudio, 5),
	}
	for i := range atLimit.images {
		atLimit.images[i] = service.R2VMediaImage{URL: "img"}
	}
	for i := range atLimit.videos {
		atLimit.videos[i] = service.R2VMediaVideo{URL: "vid"}
	}
	for i := range atLimit.audios {
		atLimit.audios[i] = service.R2VMediaAudio{URL: "aud"}
	}
	_, err = svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeR2V, Prompt: "x",
		Images: atLimit.images, Videos: atLimit.videos, Audios: atLimit.audios,
	})
	require.NoError(t, err, "合计 20 条但每类都在上限内，必须通过")
	assert.Len(t, mediaOf(t, factory), 20)
}

// 参考音频只有 wan3.0 认识；wan2.7/happyhorse 收到必须报错。
func TestWan30_ReferenceAudio_RejectedOnOlderModels(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  "wan2.7-r2v",
		Prompt: "x",
		Images: []service.R2VMediaImage{{URL: "img1"}},
		Audios: []service.R2VMediaAudio{{URL: "aud1"}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

func TestWan30_F2V_And_L2V_MediaShape(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	task, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:   wan30,
		Mode:    generationmodel.TaskModeF2V,
		Prompt:  "一支高端智能眼镜产品广告",
		FileURL: "https://example.com/glass.pptx",
	})
	require.NoError(t, err)
	assert.Equal(t, generationmodel.TaskModeF2V, task.Mode)
	assert.Equal(t, []map[string]any{
		{"type": "file", "url": "https://example.com/glass.pptx"},
	}, mediaOf(t, factory))

	task, err = svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:   wan30,
		Mode:    generationmodel.TaskModeL2V,
		LinkURL: "https://example.com/product",
	})
	require.NoError(t, err)
	assert.Equal(t, generationmodel.TaskModeL2V, task.Mode)
	assert.Equal(t, []map[string]any{
		{"type": "link", "url": "https://example.com/product"},
	}, mediaOf(t, factory))
}

func TestWan30_F2V_MissingFile_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:  wan30,
		Mode:   generationmodel.TaskModeF2V,
		Prompt: "x",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// f2v/l2v 只有 wan3.0 有，别的模型连这两个 mode 都不认。
func TestWan30_F2V_RejectedOnOlderModels(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model:   "wan2.7-r2v",
		Mode:    generationmodel.TaskModeF2V,
		FileURL: "https://example.com/a.pdf",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// ── 参数取值范围现在按模型走目录 ─────────────────────────────────────

// 480P 只有 wan3.0 认；wan2.7 传 480P 必须被拒。同一个取值在两个模型上
// 一通一拒，正是"取值集合下沉到目录"这件事的意义。
func TestWan30_Resolution480P_AcceptedHere_RejectedOnWan27(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeT2V, Prompt: "x",
		Params: service.VideoParams{Resolution: "480P"},
	})
	require.NoError(t, err)
	assert.Equal(t, "480P", parametersOf(t, factory)["resolution"])

	_, err = svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: "happyhorse-1.1-t2v", Prompt: "x",
		Params: service.VideoParams{Resolution: "480P"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// 时长区间同理：wan3.0 是 [2,30]，wan2.7 是 [3,15]。
func TestWan30_DurationRange(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	for _, d := range []int{2, 30} {
		_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
			Model: wan30, Mode: generationmodel.TaskModeT2V, Prompt: "x",
			Params: service.VideoParams{Duration: d},
		})
		require.NoErrorf(t, err, "duration=%d 应在 wan3.0 的允许区间内", d)
	}

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeT2V, Prompt: "x",
		Params: service.VideoParams{Duration: 31},
	})
	require.Error(t, err)

	// 同样的 30 秒在 wan2.7 上越界。
	_, err = svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: "happyhorse-1.1-t2v", Prompt: "x",
		Params: service.VideoParams{Duration: 30},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// duration=-1 是"智能时长"哨兵值，只有 SmartDuration 的模型接受。
func TestWan30_SmartDuration(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeT2V, Prompt: "x",
		Params: service.VideoParams{Duration: -1},
	})
	require.NoError(t, err)
	assert.Equal(t, -1, parametersOf(t, factory)["duration"])

	_, err = svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: "happyhorse-1.1-t2v", Prompt: "x",
		Params: service.VideoParams{Duration: -1},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// wan3.0 的 ratio 在 i2v 下也有效（默认 adaptive 本身就是"跟随输入"），
// 与 wan2.7-i2v"带 ratio 直接拒绝"的行为正好相反——两条一起断言，防止
// 以后有人把其中一条"统一"掉。
func TestWan30_I2V_AcceptsRatio_WhileWan27Rejects(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeI2V, Prompt: "x",
		TaskType: service.I2VTaskFirstFrame, FirstFrame: "https://example.com/a.png",
		Params: service.VideoParams{Ratio: "9:16"},
	})
	require.NoError(t, err)
	assert.Equal(t, "9:16", parametersOf(t, factory)["ratio"])

	_, err = svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: "wan2.7-i2v-2026-04-25", TaskType: service.I2VTaskFirstFrame,
		FirstFrame: "https://example.com/a.png",
		Params:     service.VideoParams{Ratio: "9:16"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// 没传 ratio 时 wan3.0 用目录里的默认值 adaptive（wan2.7/happyhorse 是
// 16:9），且 i2v 也照样带上——这是与旧模型最容易被误"统一"的一处差异。
func TestWan30_DefaultsFromCatalog(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeI2V, Prompt: "x",
		TaskType: service.I2VTaskFirstFrame, FirstFrame: "https://example.com/a.png",
	})
	require.NoError(t, err)

	params := parametersOf(t, factory)
	assert.Equal(t, "adaptive", params["ratio"])
	assert.Equal(t, "720P", params["resolution"])
	assert.Equal(t, 5, params["duration"])
}

// 4:5 属于 wan2.7 的 ratio 集合，wan3.0 没有——必须被拒。
func TestWan30_RatioOutsideItsOwnSet_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeT2V, Prompt: "x",
		Params: service.VideoParams{Ratio: "4:5"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// ── audio / negative_prompt：Supports 第一次用来"减"参数 ──────────────

func TestWan30_AudioParam_SentWhenSet_OmittedWhenNil(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	off := false
	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeT2V, Prompt: "x",
		Params: service.VideoParams{Audio: &off},
	})
	require.NoError(t, err)
	assert.Equal(t, false, parametersOf(t, factory)["audio"],
		"显式关掉声音必须发出去，不能因为是 false 就被当成未设置丢掉")

	_, err = svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeT2V, Prompt: "x",
	})
	require.NoError(t, err)
	_, has := parametersOf(t, factory)["audio"]
	assert.False(t, has, "没设置时不发 audio，由上游用它自己的默认值")
}

func TestWan30_AudioParam_RejectedOnOlderModels(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	on := true
	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: "happyhorse-1.1-t2v", Prompt: "x",
		Params: service.VideoParams{Audio: &on},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// wan3.0 的 input 里没有 negative_prompt 的位置，传了要报错而不是被
// 默默塞进请求体让上游拒绝。
func TestWan30_NegativePrompt_Rejected(t *testing.T) {
	settings := dashscopeSettings(nil)
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeT2V, Prompt: "x",
		Params: service.VideoParams{NegativePrompt: "blurry"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, apperr.ErrValidation)
}

// ── 地域 ────────────────────────────────────────────────────────────

// wan3.0 不带地域白名单，而 wan2.7 在 us-east-1 会被拒——这条钉住的是
// "Regions 为空是事实不是遗漏"，顺带证明 VideoProfile 取代
// len(Regions)>0 之后 wan3.0 没有被误判成 happyhorse（否则参考视频这
// 一项就会报"不支持参考视频"）。
func TestWan30_AllRegionsAllowed(t *testing.T) {
	settings := dashscopeSettings(map[settingmodel.Key]string{settingmodel.KeyRegion: "us-east-1"})
	factory := &recordingVideoFactory{taskID: "t"}
	svc, _ := newVideoService(t, settings, factory)

	_, err := svc.CreateVideoTask(context.Background(), 1, service.CreateVideoTaskRequest{
		Model: wan30, Mode: generationmodel.TaskModeR2V, Prompt: "x",
		Videos: []service.R2VMediaVideo{{URL: "vid1"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{
		{"type": "reference_video", "url": "vid1"},
	}, mediaOf(t, factory))
}
