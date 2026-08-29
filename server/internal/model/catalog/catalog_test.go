package catalog_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogmodel "github.com/chenhao/omnigen-ai/server/internal/model/catalog"
)

// TestAll_ExactModelSet pins down every model id that appeared anywhere in the
// old system (server.js MODELS, imggen.js/imgedit.js dropdowns, r2v.js/i2v.js
// literals, index.html <option> values). If a future change adds a model in
// one place and not the catalog, this test must fail.
func TestAll_ExactModelSet(t *testing.T) {
	want := []string{
		// image models — dashscope native protocol
		"qwen-image-plus", "qwen-image", "qwen-image-edit-plus", "qwen-image-edit",
		"wan2.7-image-pro", "wan2.7-image",
		// image model — t8star / openai chat-completions protocol
		"gpt-image-2",
		// video models — only discoverable in public/js/{r2v,i2v}.js and index.html
		"happyhorse-1.1-r2v", "wan2.7-r2v",
		"happyhorse-1.1-i2v", "wan2.7-i2v-2026-04-25",
		"happyhorse-1.1-t2v",
		// wan3.0：单个 model id 覆盖 t2v/i2v/r2v/f2v/l2v 五种能力
		"wan3.0-video", "wan3.0-video-prime",
		// prompt-optimize models (+ fallbacks), from server.js MODELS block
		"qwen3.7-plus", "qwen-plus",
		"qwen-vl-max-latest", "qwen-vl-plus",
	}
	sort.Strings(want)

	all := catalogmodel.All()
	got := make([]string, 0, len(all))
	for _, m := range all {
		got = append(got, m.ID)
	}
	sort.Strings(got)

	assert.Equal(t, want, got)
}

func TestNoDuplicateIDs_AndEveryModelHasCapability(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range catalogmodel.All() {
		if seen[m.ID] {
			t.Fatalf("duplicate model id: %s", m.ID)
		}
		seen[m.ID] = true
		assert.NotEmptyf(t, m.Capabilities, "model %s has no capabilities", m.ID)
	}
}

func TestByID(t *testing.T) {
	m, ok := catalogmodel.ByID("qwen-image-plus")
	require.True(t, ok)
	assert.Equal(t, "qwen-image-plus", m.ID)

	_, ok = catalogmodel.ByID("does-not-exist")
	assert.False(t, ok)
}

// wan2.7 family is restricted to cn-beijing / ap-southeast-1 — checked
// client-side in imggen.js/imgedit.js/r2v.js/i2v.js against `auth.region`.
func TestWan27_RegionRestriction(t *testing.T) {
	wanModels := []string{
		"wan2.7-image-pro", "wan2.7-image", "wan2.7-r2v", "wan2.7-i2v-2026-04-25",
	}
	for _, id := range wanModels {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok, "missing model %s", id)

		assert.True(t, m.AllowsRegion("cn-beijing"), "%s should allow cn-beijing", id)
		assert.True(t, m.AllowsRegion("ap-southeast-1"), "%s should allow ap-southeast-1", id)
		assert.False(t, m.AllowsRegion("us-east-1"), "%s should reject us-east-1", id)
		assert.False(t, m.AllowsRegion("eu-central-1"), "%s should reject eu-central-1", id)
	}
}

// Non-wan models are unrestricted: empty Regions means "any region allowed".
func TestNonWanModels_UnrestrictedRegion(t *testing.T) {
	nonWan := []string{
		"qwen-image-plus", "qwen-image", "qwen-image-edit-plus", "qwen-image-edit",
		"gpt-image-2", "happyhorse-1.1-r2v", "happyhorse-1.1-i2v", "happyhorse-1.1-t2v",
	}
	for _, id := range nonWan {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok, "missing model %s", id)
		assert.Empty(t, m.Regions, "%s should have no region restriction", id)
		for _, r := range []string{"cn-beijing", "ap-southeast-1", "us-east-1", "eu-central-1", "anything"} {
			assert.True(t, m.AllowsRegion(r), "%s should allow region %s", id, r)
		}
	}
}

func TestGptImage2_ProtocolAndNoVideoCapability(t *testing.T) {
	m, ok := catalogmodel.ByID("gpt-image-2")
	require.True(t, ok)

	assert.Equal(t, catalogmodel.ProtocolOpenAI, m.Protocol)

	videoCaps := []catalogmodel.Capability{
		catalogmodel.CapabilityT2V, catalogmodel.CapabilityI2V, catalogmodel.CapabilityR2V,
	}
	for _, c := range videoCaps {
		assert.NotContains(t, m.Capabilities, c, "gpt-image-2 must not have capability %s", c)
	}

	// gpt-image-2 is a chat API: it ignores size/n/seed/watermark/negative_prompt entirely.
	assert.Empty(t, m.Sizes)
	assert.Empty(t, m.Supports)
}

func TestDashScopeModels_Protocol(t *testing.T) {
	ids := []string{
		"qwen-image-plus", "qwen-image", "qwen-image-edit-plus", "qwen-image-edit",
		"wan2.7-image-pro", "wan2.7-image",
		"happyhorse-1.1-r2v", "wan2.7-r2v",
		"happyhorse-1.1-i2v", "wan2.7-i2v-2026-04-25",
		"happyhorse-1.1-t2v",
		"qwen3.7-plus", "qwen-plus", "qwen-vl-max-latest", "qwen-vl-plus",
	}
	for _, id := range ids {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok, "missing model %s", id)
		assert.Equal(t, catalogmodel.ProtocolDashScope, m.Protocol, "model %s should be dashscope protocol", id)
	}
}

// Per-model MaxN, matching the old count-option rules in imggen.js/imgedit.js.
func TestMaxN_ImageModels(t *testing.T) {
	cases := []struct {
		id             string
		maxN           int
		sequentialMaxN int
	}{
		{"qwen-image-plus", 1, 0},
		{"qwen-image", 1, 0},
		{"qwen-image-edit-plus", 6, 0},
		{"qwen-image-edit", 1, 0},
		{"wan2.7-image-pro", 4, 12},
		{"wan2.7-image", 4, 12},
		{"gpt-image-2", 1, 0},
	}
	for _, c := range cases {
		m, ok := catalogmodel.ByID(c.id)
		require.True(t, ok, "missing model %s", c.id)
		assert.Equalf(t, c.maxN, m.MaxN, "MaxN for %s", c.id)
		assert.Equalf(t, c.sequentialMaxN, m.SequentialMaxN, "SequentialMaxN for %s", c.id)
	}
}

func TestByCapability(t *testing.T) {
	cases := []struct {
		cap  catalogmodel.Capability
		want []string
	}{
		{catalogmodel.CapabilityT2I, []string{"qwen-image-plus", "qwen-image", "wan2.7-image-pro", "wan2.7-image", "gpt-image-2"}},
		{catalogmodel.CapabilityEdit, []string{"qwen-image-edit-plus", "qwen-image-edit", "wan2.7-image-pro", "wan2.7-image", "gpt-image-2"}},
		{catalogmodel.CapabilityT2V, []string{"happyhorse-1.1-t2v", "wan3.0-video", "wan3.0-video-prime"}},
		{catalogmodel.CapabilityI2V, []string{"happyhorse-1.1-i2v", "wan2.7-i2v-2026-04-25", "wan3.0-video", "wan3.0-video-prime"}},
		{catalogmodel.CapabilityR2V, []string{"happyhorse-1.1-r2v", "wan2.7-r2v", "wan3.0-video", "wan3.0-video-prime"}},
		{catalogmodel.CapabilityF2V, []string{"wan3.0-video", "wan3.0-video-prime"}},
		{catalogmodel.CapabilityL2V, []string{"wan3.0-video", "wan3.0-video-prime"}},
		{catalogmodel.CapabilityOptimizeText, []string{"qwen3.7-plus", "qwen-plus"}},
		{catalogmodel.CapabilityOptimizeVision, []string{"qwen-vl-max-latest", "qwen-vl-plus"}},
	}
	for _, c := range cases {
		models := catalogmodel.ByCapability(c.cap)
		got := make([]string, 0, len(models))
		for _, m := range models {
			got = append(got, m.ID)
		}
		sort.Strings(got)
		want := append([]string(nil), c.want...)
		sort.Strings(want)
		assert.Equalf(t, want, got, "ByCapability(%s)", c.cap)
	}
}

// thinking_mode / enable_sequential are wan-image-only params (imggen.js /
// imgedit.js only send them `if (isWanModel(model))`).
func TestSupportsParam_ThinkingModeAndSequentialOnlyWanImage(t *testing.T) {
	wanImage := []string{"wan2.7-image-pro", "wan2.7-image"}
	for _, id := range wanImage {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok)
		assert.True(t, m.SupportsParam("thinking_mode"), "%s should support thinking_mode", id)
		assert.True(t, m.SupportsParam("enable_sequential"), "%s should support enable_sequential", id)
	}

	others := []string{}
	for _, m := range catalogmodel.All() {
		if m.ID == "wan2.7-image-pro" || m.ID == "wan2.7-image" {
			continue
		}
		others = append(others, m.ID)
	}
	for _, id := range others {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok)
		assert.False(t, m.SupportsParam("thinking_mode"), "%s should not support thinking_mode", id)
		assert.False(t, m.SupportsParam("enable_sequential"), "%s should not support enable_sequential", id)
	}
}

// negative_prompt / prompt_extend for video are wan-only (r2v.js / i2v.js
// only add them inside `if (wan) { ... }`).
func TestSupportsParam_NegativePromptAndPromptExtend_VideoWanOnly(t *testing.T) {
	wanVideo := []string{"wan2.7-r2v", "wan2.7-i2v-2026-04-25"}
	for _, id := range wanVideo {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok)
		assert.True(t, m.SupportsParam("negative_prompt"), "%s should support negative_prompt", id)
		assert.True(t, m.SupportsParam("prompt_extend"), "%s should support prompt_extend", id)
	}

	happyhorseVideo := []string{"happyhorse-1.1-r2v", "happyhorse-1.1-i2v", "happyhorse-1.1-t2v"}
	for _, id := range happyhorseVideo {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok)
		assert.False(t, m.SupportsParam("negative_prompt"), "%s should not support negative_prompt", id)
		assert.False(t, m.SupportsParam("prompt_extend"), "%s should not support prompt_extend", id)
	}
}

// seed and watermark are common to every video model (task.js collectParams
// applies to all of t2v/i2v/r2v unconditionally).
func TestSupportsParam_SeedAndWatermark_AllVideoModels(t *testing.T) {
	ids := []string{"happyhorse-1.1-r2v", "wan2.7-r2v", "happyhorse-1.1-i2v", "wan2.7-i2v-2026-04-25", "happyhorse-1.1-t2v"}
	for _, id := range ids {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok)
		assert.True(t, m.SupportsParam("seed"), "%s should support seed", id)
		assert.True(t, m.SupportsParam("watermark"), "%s should support watermark", id)
	}
}

// i2v client-side validation (i2v.js bindSingleImageUploader): both width AND
// height must meet the minimum edge (not the short side), and the aspect
// ratio must fall in [ratioMin, ratioMax].
func TestI2V_DimensionRatioLimits(t *testing.T) {
	cases := []struct {
		id           string
		minImageEdge int
		ratioMin     float64
		ratioMax     float64
	}{
		{"happyhorse-1.1-i2v", 300, 0.4, 2.5},
		{"wan2.7-i2v-2026-04-25", 240, 0.125, 8},
		// wan3.0 的图片规格与 wan2.7 相同：单边 [240, 8000]、长宽比 ≤8:1。
		{"wan3.0-video", 240, 0.125, 8},
		{"wan3.0-video-prime", 240, 0.125, 8},
	}
	for _, c := range cases {
		m, ok := catalogmodel.ByID(c.id)
		require.True(t, ok)
		assert.Equalf(t, c.minImageEdge, m.MinImageEdge, "MinImageEdge for %s", c.id)
		assert.InDeltaf(t, c.ratioMin, m.RatioMin, 1e-9, "RatioMin for %s", c.id)
		assert.InDeltaf(t, c.ratioMax, m.RatioMax, 1e-9, "RatioMax for %s", c.id)
	}

	// 反向断言按能力而不是按 id 列表写：这几个字段的存在理由就是"该模型
	// 接受首帧图片"，所以凡是不具备 i2v 能力的模型都必须为零值。用能力
	// 判断而不是"排除上面列出的几个 id"，新增 i2v 模型时不会因为忘了往
	// 排除名单里补一行而误报。
	for _, m := range catalogmodel.All() {
		if m.HasCapability(catalogmodel.CapabilityI2V) {
			continue
		}
		assert.Zerof(t, m.MinImageEdge, "MinImageEdge for %s should be zero", m.ID)
	}
}

// Image upload limits: imgedit allows 3 for qwen-edit family, 9 for wan;
// r2v allows 9 for happyhorse. wan r2v's limit is dynamic (5 - videoCount)
// client-side, so the catalog records the combined image+video ceiling (5).
func TestMaxImages(t *testing.T) {
	cases := []struct {
		id        string
		maxImages int
	}{
		{"qwen-image-edit-plus", 3},
		{"qwen-image-edit", 3},
		{"wan2.7-image-pro", 9},
		{"wan2.7-image", 9},
		{"gpt-image-2", 3},
		{"happyhorse-1.1-r2v", 9},
		{"wan2.7-r2v", 5},
		{"happyhorse-1.1-i2v", 1},
		{"wan2.7-i2v-2026-04-25", 2},
	}
	for _, c := range cases {
		m, ok := catalogmodel.ByID(c.id)
		require.True(t, ok)
		assert.Equalf(t, c.maxImages, m.MaxImages, "MaxImages for %s", c.id)
	}
}

func TestSizes(t *testing.T) {
	qwenSizes := []string{"1664*928", "1472*1140", "1328*1328", "1140*1472", "928*1664"}
	qwenModels := []string{"qwen-image-plus", "qwen-image", "qwen-image-edit-plus", "qwen-image-edit"}
	for _, id := range qwenModels {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok)
		assert.Equal(t, qwenSizes, m.Sizes, "Sizes for %s", id)
	}

	wanSizes := []string{"1K", "2K", "4K"}
	wanModels := []string{"wan2.7-image-pro", "wan2.7-image"}
	for _, id := range wanModels {
		m, ok := catalogmodel.ByID(id)
		require.True(t, ok)
		assert.Equal(t, wanSizes, m.Sizes, "Sizes for %s", id)
	}

	for _, m := range catalogmodel.All() {
		for _, cap := range m.Capabilities {
			if cap == catalogmodel.CapabilityT2V || cap == catalogmodel.CapabilityI2V || cap == catalogmodel.CapabilityR2V {
				assert.Emptyf(t, m.Sizes, "video model %s should have no sizes", m.ID)
			}
		}
	}
}

func TestAllowsRegion_EmptyMeansUnrestricted(t *testing.T) {
	m := catalogmodel.Model{ID: "x", Regions: nil}
	assert.True(t, m.AllowsRegion("anything"))
}
