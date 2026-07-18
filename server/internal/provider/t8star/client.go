package t8star

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
)

// upstreamTimeout mirrors the 180000ms passed to forwardJSON() in the old
// server.js /api/generate-image handler.
const upstreamTimeout = 180 * time.Second

// Client is the t8star ImageProvider implementation, speaking the OpenAI
// chat-completions protocol against POST {base}/v1/chat/completions.
//
// Like dashscope.Client, credentials are fixed at construction time rather
// than passed per-call. t8star has no separate region/workspace concept —
// it reuses the endpoint/apiKey fields wholesale (see
// docs/superpowers/specs/2026-07-18-t8star-gpt-image-2-design.md).
type Client struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// New constructs a Client bound to a fixed apiKey/endpoint pair.
func New(apiKey, endpoint string) *Client {
	return &Client{
		apiKey:     apiKey,
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: upstreamTimeout},
	}
}

var _ provider.ImageProvider = (*Client)(nil)

// Result is GenerateImageWithNote's return type: provider.ImageResult plus
// the leftover prose t8star embeds alongside the image link. See
// GenerateImage's doc comment for why this doesn't fit provider.ImageResult
// directly.
type Result struct {
	provider.ImageResult
	// Note is the assistant's commentary with markdown image links
	// stripped and trimmed (e.g. "给你画好了，一只可爱的小猫。"). The old
	// system renders this below the result image — it's real model output,
	// not incidental noise.
	Note string
}

// GenerateImage implements provider.ImageProvider.
//
// provider.ImageResult (defined in provider.go, owned by a different task)
// 现在 provider.ImageResult 已经有 Note 字段（协调后新增），所以通用接口
// 也能带出 t8star 的散文说明，service 层不必为 t8star 单独走一条分支。
// GenerateImageWithNote 保留下来，给明确知道自己在跟 t8star 说话、
// 想拿到强类型 Result 的调用方用。
func (c *Client) GenerateImage(ctx context.Context, req provider.ImageRequest) (*provider.ImageResult, error) {
	result, err := c.GenerateImageWithNote(ctx, req)
	if err != nil {
		return nil, err
	}
	out := result.ImageResult
	out.Note = result.Note
	return &out, nil
}

// GenerateImageWithNote posts a chat-completions request and parses the
// markdown image link plus the leftover prose out of the reply, porting the
// t8star branch of POST /api/generate-image in the old server.js verbatim:
//   - HTTP status >= 400 -> CodeUpstreamHTTP, forwarding the real status
//   - 200 OK with an `error` body -> CodeUpstreamError (400)
//   - 200 OK, parses clean, but zero images found -> CodeNoImageResult (502)
//
// req.Params is ignored: t8star's chat-completions endpoint has no
// size/n/seed/watermark/negative_prompt parameters, so nothing from
// provider.ImageParams is ever sent upstream.
func (c *Client) GenerateImageWithNote(ctx context.Context, req provider.ImageRequest) (*Result, error) {
	base := ResolveBaseURL(c.endpoint)
	url := base + "/v1/chat/completions"
	payload := BuildPayload(req.Model, req.Prompt, req.Images)

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("t8star: marshal payload: %w", err))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(fmt.Errorf("t8star: build request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, newUpstreamHTTPError(http.StatusBadGateway, fmt.Errorf("t8star: request failed: %w", err))
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, newUpstreamHTTPError(http.StatusBadGateway, fmt.Errorf("t8star: read response body: %w", err))
	}

	var data Response
	// A non-JSON body is tolerated here (mirrors JS, where a parse failure
	// just leaves data's optional fields undefined) rather than treated as
	// fatal on its own — the status/error checks below still apply.
	_ = json.Unmarshal(respBody, &data)

	if httpResp.StatusCode >= 400 {
		msg := upstreamErrorMessage(data)
		if msg == "" {
			msg = fmt.Sprintf("t8star upstream returned HTTP %d", httpResp.StatusCode)
		}
		return nil, newUpstreamHTTPError(httpResp.StatusCode, fmt.Errorf("%s", msg))
	}

	if data.Error != nil {
		msg := data.Error.Message
		if msg == "" {
			msg = "t8star call failed"
		}
		return nil, ErrUpstreamError.Wrap(fmt.Errorf("%s", msg))
	}

	images, note := ParseResponse(data)
	if len(images) == 0 {
		return nil, ErrNoImageResult.Wrap(fmt.Errorf("t8star: no image found in response"))
	}

	model := data.Model
	if model == "" {
		model = req.Model
	}

	return &Result{
		ImageResult: provider.ImageResult{
			Images: images,
			Usage:  data.Usage,
			Model:  model,
		},
		Note: note,
	}, nil
}

// upstreamErrorMessage mirrors:
//
//	t8r.data?.error?.message || t8r.data?.message || <generic>
func upstreamErrorMessage(data Response) string {
	if data.Error != nil && data.Error.Message != "" {
		return data.Error.Message
	}
	return data.Message
}
