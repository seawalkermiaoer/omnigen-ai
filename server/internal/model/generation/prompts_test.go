package generation_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/model/generation"
)

// expectedPromptSHA256 是用一次性脚本，从 server.js:443-550 的
// `const SYSTEM_PROMPTS = { ... };` 里，逐个把每个模板字符串字面量的内容
// eval 出来、按 UTF-8 字节算 sha256 得到的结果（脚本：先按 `const
// SYSTEM_PROMPTS = {` 与其后第一个 `\n};` 切出对象体，再包一层
// `({...})` 用 Node 的 eval 求值——纯数据求值，不执行任何副作用；随后把
// 每个 value 原样、不做任何 trim/normalize 地写入
// server/internal/model/generation/promptdata/<mode>.txt，这些 .txt 文件
// 就是 prompts.go 用 go:embed 编译进二进制的内容）。
//
// 这里把提取结果固化成常量，而不是每次跑测试都重新解析 server.js：
// 一来 prompts.go 已经通过 go:embed 把 promptdata/*.txt 锁死为常量，
// 运行期不可能再漂移；二来重新解析 server.js 会让本包的测试对
// server.js 的文件结构（marker 位置、周围代码有没有搬动）产生一条隐式
// 依赖，那类失败和"提示词文字真的被改了"是两种不同性质的问题，不应该用
// 同一个断言表达。sha256 是当前这次移植时刻的快照，后续如果 server.js
// 的提示词文案本身发生改动（而不是这次移植出的 bug），需要同步重新生成
// promptdata/*.txt 与这份期望值。
var expectedPromptSHA256 = map[generation.OptimizeMode]string{
	generation.ModeR2V:        "dd437a342e07caa5ffe6a39e22a9fa84b72cc2d5c0d92e9a07dc8ea083b55cc3",
	generation.ModeT2V:        "1294b534b656265def638f734478d1c9e51975907433eead69536aca00897ee3",
	generation.ModeR2VWan:     "95103cba5bebc4d5674eb5846fc8277886e90991297ec824708b59f0152ab0fc",
	generation.ModeI2VWan:     "a5fb6326b8fa307e197ff7c418ab2075f6beb5960b1e005485f7a758d63f8de1",
	generation.ModeI2V:        "f9c7bd82471e1890d75261833e4a73d43736e7148760ba814be40af17f77d6c5",
	generation.ModeT2I:        "6d3edfc6626a4dd3490e11e74d82970e99f87dd2eb7651eebf72f31c64130843",
	generation.ModeImggenEdit: "c687653d2fb5adea59234674131edb310e0fc7ce87259c517a2e26fc854149e4",
}

// expectedPromptByteLen 是同一次提取脚本报告的 UTF-8 字节数，作为 sha256
// 之外的第二重、更容易凭肉眼核对的信号：字节数对不上，肯定不是同一段文字；
// 字节数对上但 hash 不对，说明内容重排/替换但长度恰好相同——概率极低，
// 但两个信号一起断言比单独任何一个都更难被巧合绕过。
var expectedPromptByteLen = map[generation.OptimizeMode]int{
	generation.ModeR2V:        1000,
	generation.ModeT2V:        752,
	generation.ModeR2VWan:     1182,
	generation.ModeI2VWan:     1535,
	generation.ModeI2V:        1146,
	generation.ModeT2I:        885,
	generation.ModeImggenEdit: 949,
}

func TestSystemPrompts_ByteIdenticalToServerJS(t *testing.T) {
	require.Len(t, generation.SystemPrompts, len(expectedPromptSHA256), "SystemPrompts 的 key 数量应该恰好是 7 个")

	for mode, wantHash := range expectedPromptSHA256 {
		prompt, ok := generation.SystemPrompts[mode]
		require.Truef(t, ok, "SystemPrompts 缺少 mode %q", mode)

		assert.Equalf(t, expectedPromptByteLen[mode], len(prompt),
			"mode %q 的提示词字节数与 server.js 提取快照不一致", mode)

		sum := sha256.Sum256([]byte(prompt))
		got := hex.EncodeToString(sum[:])
		assert.Equalf(t, wantHash, got,
			"mode %q 的提示词 sha256 与 server.js 提取快照不一致，说明文字发生了漂移", mode)
	}
}

// TestSystemPrompts_EachModeSelectsDistinctPrompt 用每个提示词里独有的
// "对应哪个具体模型名/协议" 片段做指纹，防止 7 套提示词在搬运过程中被
// 复制粘贴串位（例如把 i2v 的内容错填成了 i2v_wan 的 key）。
func TestSystemPrompts_EachModeSelectsDistinctPrompt(t *testing.T) {
	cases := []struct {
		mode      generation.OptimizeMode
		substring string
	}{
		{generation.ModeR2V, "happyhorse-1.1-r2v"},
		{generation.ModeT2V, "happyhorse-1.1-t2v"},
		{generation.ModeR2VWan, "wan2.7-r2v"},
		{generation.ModeI2VWan, "wan2.7-i2v"},
		{generation.ModeI2V, "happyhorse-1.1-i2v"},
		{generation.ModeT2I, "qwen-image / wan2.7-image"},
		{generation.ModeImggenEdit, "qwen-image-edit / wan2.7-image"},
	}

	for _, tc := range cases {
		prompt, ok := generation.SystemPrompts[tc.mode]
		require.Truef(t, ok, "SystemPrompts 缺少 mode %q", tc.mode)
		assert.Containsf(t, prompt, tc.substring,
			"mode %q 的提示词里找不到区分它的特征片段 %q，可能被复制粘贴串位了", tc.mode, tc.substring)
	}

	// 两两互不相同：7 个 key 对应 7 段互不相同的文本。
	seen := make(map[string]generation.OptimizeMode, len(cases))
	for _, tc := range cases {
		prompt := generation.SystemPrompts[tc.mode]
		if other, dup := seen[prompt]; dup {
			t.Fatalf("mode %q 与 mode %q 解析出完全相同的提示词文本", tc.mode, other)
		}
		seen[prompt] = tc.mode
	}
}

// ── mode 解析：未知 mode 回退 t2v；imgedit 别名修 bug ────────────────────

func TestResolveMode_KnownModesResolveToThemselvesWithoutFallback(t *testing.T) {
	known := []generation.OptimizeMode{
		generation.ModeR2V, generation.ModeT2V, generation.ModeR2VWan,
		generation.ModeI2VWan, generation.ModeI2V, generation.ModeT2I, generation.ModeImggenEdit,
	}
	for _, m := range known {
		mode, fellBack := generation.ResolveMode(string(m))
		assert.Equal(t, m, mode)
		assert.Falsef(t, fellBack, "已知 mode %q 不应该触发 fallback", m)
	}
}

func TestResolveMode_UnknownModeFallsBackToT2V(t *testing.T) {
	for _, raw := range []string{"", "bogus-mode", "T2V", "R2V", "video-edit"} {
		mode, fellBack := generation.ResolveMode(raw)
		assert.Equalf(t, generation.ModeT2V, mode, "未知 mode %q 应该回退到 t2v", raw)
		assert.Truef(t, fellBack, "未知 mode %q 的 fellBack 应该为 true，调用方要据此打日志", raw)
	}
}

// TestResolveMode_ImgeditAliasResolvesToImggenEditNeverT2V 覆盖旧 bug：
// public/js/task.js 在图片编辑 tab 没有图片时，会把 mode 留成初始值
// 'imgedit'（不是合法 key），旧服务端因此静默落到 t2v——用文生视频提示词
// 优化一个图片编辑请求。新实现必须把 'imgedit' 显式映射到 imggen_edit，
// 且不经过 fellBack=true 的通用兜底路径。
func TestResolveMode_ImgeditAliasResolvesToImggenEditNeverT2V(t *testing.T) {
	mode, fellBack := generation.ResolveMode("imgedit")
	assert.Equal(t, generation.ModeImggenEdit, mode)
	assert.NotEqual(t, generation.ModeT2V, mode)
	assert.False(t, fellBack, "imgedit 是已知别名，不应该走 fallback 路径")
}

func TestPromptForMode_ImgeditReturnsImageEditPromptNeverT2VPrompt(t *testing.T) {
	prompt, mode, fellBack := generation.PromptForMode("imgedit")
	assert.Equal(t, generation.ModeImggenEdit, mode)
	assert.False(t, fellBack)
	assert.Equal(t, generation.SystemPrompts[generation.ModeImggenEdit], prompt)
	assert.NotEqual(t, generation.SystemPrompts[generation.ModeT2V], prompt)
	assert.Contains(t, prompt, "图片编辑")
}

func TestPromptForMode_UnknownModeReturnsT2VPrompt(t *testing.T) {
	prompt, mode, fellBack := generation.PromptForMode("does-not-exist")
	assert.Equal(t, generation.ModeT2V, mode)
	assert.True(t, fellBack)
	assert.Equal(t, generation.SystemPrompts[generation.ModeT2V], prompt)
}
