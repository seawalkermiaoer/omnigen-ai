// Package generation 目前只承载 Task 8（prompt 优化）需要的类型；
// generation_tasks 的表结构相关类型（types/request/response）由并行的另一个
// 任务补齐，不在本文件职责范围内。
package generation

import _ "embed"

// OptimizeMode 是 /api/optimize-prompt 的 mode 参数取值，对应 server.js:443-550
// 里 SYSTEM_PROMPTS 的 7 个 key。类型化成一个独立类型（而不是到处传裸
// string）是为了让 "只有这 7 个值合法" 这件事在编译期可见——ResolveMode 是
// 唯一允许从任意字符串产出 OptimizeMode 的入口。
type OptimizeMode string

const (
	ModeR2V        OptimizeMode = "r2v"
	ModeT2V        OptimizeMode = "t2v"
	ModeR2VWan     OptimizeMode = "r2v_wan"
	ModeI2VWan     OptimizeMode = "i2v_wan"
	ModeI2V        OptimizeMode = "i2v"
	ModeT2I        OptimizeMode = "t2i"
	ModeImggenEdit OptimizeMode = "imggen_edit"
)

// 7 套系统提示词，逐字节从 server.js:443-550 搬过来。
//
// 之所以用 go:embed 从 promptdata/*.txt 读取，而不是把中文段落直接写成 Go
// 源码里的反引号字符串字面量，是为了让"这段文字与 server.js 字节相同"这件
// 事不依赖人工转录（哪怕只是复制粘贴，中文标点、全角/半角符号、不可见的
// U+3000 之类也有被编辑器悄悄改写的风险）。promptdata/*.txt 是用脚本从
// server.js 里原样切出来的（跳过 `const SYSTEM_PROMPTS = {` 与结尾 `};`
// 之间的每个模板字符串字面量，逐字节写入同名文件，不做任何 trim/normalize），
// go:embed 再把这些文件原样编译进二进制。prompts_test.go 用 sha256 校验每个
// 文件与当前 server.js 里对应字面量一致，这样"文字被改动"这件事在测试里
// 就是可检测的，而不是只能靠人眼比对。
//
//go:embed promptdata/r2v.txt
var promptR2V string

//go:embed promptdata/t2v.txt
var promptT2V string

//go:embed promptdata/r2v_wan.txt
var promptR2VWan string

//go:embed promptdata/i2v_wan.txt
var promptI2VWan string

//go:embed promptdata/i2v.txt
var promptI2V string

//go:embed promptdata/t2i.txt
var promptT2I string

//go:embed promptdata/imggen_edit.txt
var promptImggenEdit string

// SystemPrompts 是 mode -> 系统提示词的映射，key 集合与 server.js 的
// SYSTEM_PROMPTS 完全一致。
var SystemPrompts = map[OptimizeMode]string{
	ModeR2V:        promptR2V,
	ModeT2V:        promptT2V,
	ModeR2VWan:     promptR2VWan,
	ModeI2VWan:     promptI2VWan,
	ModeI2V:        promptI2V,
	ModeT2I:        promptT2I,
	ModeImggenEdit: promptImggenEdit,
}

// modeAliases 修正客户端已知会发送、但从不该落到"未知 mode"通用兜底逻辑
// 上的历史命名。
//
// 旧 bug：public/js/task.js:260-265 里，图片编辑 tab 只在
// `ctx.state.images.length` 非空时才把 mode 设成 'imggen_edit'；没有图片
// 时 mode 保持初始值 'imgedit'（tab 的 kind），而 'imgedit' 不是
// SYSTEM_PROMPTS 的合法 key，旧服务端 `SYSTEM_PROMPTS[mode] || SYSTEM_PROMPTS.t2v`
// 会静默落到 t2v——用一段文生视频的提示词去优化图片编辑请求。
// 这里显式把 'imgedit' 映射到 ModeImggenEdit，让"图片编辑"这个语义
// 无论有没有图都用同一套提示词，不再经由通用未知 mode 兜底路径。
var modeAliases = map[string]OptimizeMode{
	"imgedit": ModeImggenEdit,
}

// ResolveMode 把请求里的原始 mode 字符串解析成一个 SystemPrompts 保证存在
// 的 key。
//
//   - modeAliases 命中时优先：目前只有 "imgedit" -> ModeImggenEdit 一条，
//     用来修上面说的旧 bug，不经过 fellBack 分支。
//   - 其余情况下，rawMode 若本身就是 7 个合法 key 之一，原样返回。
//   - 否则（包括空字符串、完全陌生的 mode）落到 ModeT2V，并把 fellBack
//     置为 true——旧代码这里是 `SYSTEM_PROMPTS[mode] || SYSTEM_PROMPTS.t2v`
//     的静默兜底，调用方（service 层）应该在 fellBack 为 true 时记一条日志，
//     不能像旧代码一样悄无声息。
func ResolveMode(rawMode string) (mode OptimizeMode, fellBack bool) {
	if alias, ok := modeAliases[rawMode]; ok {
		return alias, false
	}
	candidate := OptimizeMode(rawMode)
	if _, ok := SystemPrompts[candidate]; ok {
		return candidate, false
	}
	return ModeT2V, true
}

// PromptForMode 是 ResolveMode + 查表的便捷封装，返回值里的 mode 就是实际
// 生效、进而应该被用于协议/素材标签选择的那个 mode（而不是原始请求参数）。
func PromptForMode(rawMode string) (prompt string, mode OptimizeMode, fellBack bool) {
	mode, fellBack = ResolveMode(rawMode)
	return SystemPrompts[mode], mode, fellBack
}
