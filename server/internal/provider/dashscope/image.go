package dashscope

import (
	"context"
	"net/http"

	"github.com/chenhao/omnigen-ai/server/internal/provider"
)

// multimodalGenerationPath 是同步图片生成/编辑接口的路径
// （server.js:377），Optimize 的"有图"分支（optimize.go）复用同一条路径。
const multimodalGenerationPath = "/api/v1/services/aigc/multimodal-generation/generation"

// GenerateImage 照抄 server.js `/api/generate-image` 的 DashScope 分支
// （server.js:376-437）。
func (c *Client) GenerateImage(ctx context.Context, req provider.ImageRequest) (*provider.ImageResult, error) {
	base, err := c.baseURL()
	if err != nil {
		return nil, err
	}
	url := base + multimodalGenerationPath

	payload := map[string]any{
		"model": req.Model,
		"input": map[string]any{
			"messages": []map[string]any{
				{"role": "user", "content": buildImageContent(req.Images, req.Prompt)},
			},
		},
		// parameters 恒存在，哪怕是空对象——server.js:389 `parameters: {}`，
		// 之后逐个字段按条件追加，不是"有可选参数才带上 parameters 这个键"。
		"parameters": buildImageParameters(req.Params),
	}

	status, data, err := c.doRequest(ctx, http.MethodPost, url, c.authHeader(), payload, imageGenTimeout)
	if err != nil {
		return nil, ErrRequestFailed.Wrap(err)
	}

	// 上游 HTTP 错误（非 2xx）——server.js:407-410。
	if status >= 400 {
		return nil, newUpstreamHTTPError(status, data)
	}
	// DashScope 原生错误格式（200 但带 code/error）——server.js:413-416。
	if hasNativeError(data) {
		return nil, newUpstreamError(data)
	}

	images := extractImagesFromChoices(data)
	if len(images) == 0 {
		return nil, ErrNoImageResult
	}

	usage, _ := data["usage"].(map[string]any)
	return &provider.ImageResult{Images: images, Usage: usage, Model: req.Model}, nil
}

// buildImageContent 照抄 server.js:379-383：先按顺序把每张图包成
// {image: url}，再在末尾追加 {text: prompt}（prompt 非空时才追加）——
// 图在前、文在后，这个顺序是协议要求，与 t8star（文在前图在后）刻意相反，
// 不得"统一"。
func buildImageContent(images []string, prompt string) []map[string]any {
	content := make([]map[string]any, 0, len(images)+1)
	for _, img := range images {
		content = append(content, map[string]any{"image": img})
	}
	if prompt != "" {
		content = append(content, map[string]any{"text": prompt})
	}
	return content
}

// buildImageParameters 照抄 server.js:391-398 的判空条件：size/negative_prompt
// 用真值判断（空字符串等价于未提供，不写入），其余字段用 `!= null`（指针非
// nil 就写入，哪怕值是 0/false）。
func buildImageParameters(p provider.ImageParams) map[string]any {
	parameters := map[string]any{}
	if p.Size != "" {
		parameters["size"] = p.Size
	}
	if p.N != nil {
		parameters["n"] = *p.N
	}
	if p.Watermark != nil {
		parameters["watermark"] = *p.Watermark
	}
	if p.ThinkingMode != nil {
		parameters["thinking_mode"] = *p.ThinkingMode
	}
	if p.Seed != nil {
		parameters["seed"] = *p.Seed
	}
	if p.EnableSequential != nil {
		parameters["enable_sequential"] = *p.EnableSequential
	}
	if p.NegativePrompt != "" {
		parameters["negative_prompt"] = p.NegativePrompt
	}
	if p.PromptExtend != nil {
		parameters["prompt_extend"] = *p.PromptExtend
	}
	return parameters
}

// extractImagesFromChoices 照抄 server.js:419-430 的解析逻辑：
//
//	const choices = r.data?.output?.choices;
//	if (Array.isArray(choices) && choices.length > 0) {
//	  const msgContent = choices[0]?.message?.content;
//	  if (Array.isArray(msgContent)) {
//	    imageUrls = msgContent
//	      .filter(c => c.image || c.type === 'image')
//	      .map(c => c.image)
//	      .filter(Boolean);
//	  }
//	}
//
// 注意 `.filter(c => c.image || c.type === 'image')` 里 `c.type ===
// 'image'` 这个分支其实是死代码：紧接着的 `.map(c => c.image)` 只取
// `.image` 字段，哪怕某项因为 type==='image' 通过了第一次 filter，只要它
// 没有 `.image` 字段，映射结果就是 undefined，会被最后的
// `.filter(Boolean)` 去掉。因此最终效果等价于"只收集 c.image 非空的项"，
// 这里直接实现这个等价形式，不复刻死代码分支。
func extractImagesFromChoices(data map[string]any) []string {
	output := nestedMap(data, "output")
	if output == nil {
		return nil
	}
	choicesRaw, ok := output["choices"].([]any)
	if !ok || len(choicesRaw) == 0 {
		return nil
	}
	choice0, ok := choicesRaw[0].(map[string]any)
	if !ok {
		return nil
	}
	message := nestedMap(choice0, "message")
	if message == nil {
		return nil
	}
	contentRaw, ok := message["content"].([]any)
	if !ok {
		return nil
	}

	var images []string
	for _, item := range contentRaw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if img := stringField(m, "image"); img != "" {
			images = append(images, img)
		}
	}
	return images
}
