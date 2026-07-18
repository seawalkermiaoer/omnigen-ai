package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// optimizePromptRequestBody is the POST /api/optimize-prompt JSON body.
// Field names mirror the old server.js handler's `req.body` destructuring
// (server.js:555) minus the credential fields (apiKey/region/workspaceId/
// endpoint) — those come from server-side settings now, never from the
// client, same trust-boundary shift service.OptimizeRequest's doc comment
// explains. Mode is passed through completely unvalidated: service.Optimize
// (via generation.PromptForMode) owns alias resolution ("imgedit" ->
// imggen_edit) and the unknown-mode fallback to t2v.
type optimizePromptRequestBody struct {
	Draft      string   `json:"draft"`
	Images     []string `json:"images"`
	Mode       string   `json:"mode"`
	VideoCount int      `json:"videoCount"`
}

// optimizePromptResponse mirrors the old system's `res.json({ prompt, model })`
// (server.js:648) — the old UI displays which model actually produced the
// text, since the degrade chain may have fallen back from the primary model
// to one of its fallbacks.
type optimizePromptResponse struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
}

// OptimizeHandler wires POST /api/optimize-prompt to service.OptimizeService.
// Like ImageGenerationHandler/VideoGenerationHandler, it does binding/
// validation/delegation only — every business rule (system prompt selection,
// sourceDesc/draftText assembly, the model/endpoint degrade chain) lives in
// the service.
type OptimizeHandler struct {
	optimize *service.OptimizeService
}

func NewOptimizeHandler(optimize *service.OptimizeService) *OptimizeHandler {
	return &OptimizeHandler{optimize: optimize}
}

// Optimize handles POST /api/optimize-prompt for any logged-in user. There's
// no per-user business state involved (unlike image/video generation, this
// endpoint doesn't persist a task), so the authenticated userID is only
// consulted to gate access — it's never threaded into service.OptimizeRequest.
func (h *OptimizeHandler) Optimize(c *gin.Context) {
	if _, ok := middleware.UserIDFrom(c); !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	var body optimizePromptRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}

	text, actualModel, err := h.optimize.Optimize(c.Request.Context(), service.OptimizeRequest{
		Mode:       body.Mode,
		Draft:      body.Draft,
		Images:     body.Images,
		VideoCount: body.VideoCount,
	})
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	c.JSON(http.StatusOK, common.OK(optimizePromptResponse{Prompt: text, Model: actualModel}))
}
