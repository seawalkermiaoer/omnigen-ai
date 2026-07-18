package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/provider"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// generateImageRequestBody is the POST /api/generate/image JSON body.
// Fields mirror provider.ImageParams (which has no json tags of its own,
// since it's shared with the video-side request shapes that don't bind
// straight off HTTP) — this is the one place that maps wire field names to
// it.
type generateImageRequestBody struct {
	Model  string   `json:"model" binding:"required"`
	Prompt string   `json:"prompt"`
	Images []string `json:"images"`

	Size             string `json:"size"`
	NegativePrompt   string `json:"negativePrompt"`
	N                *int   `json:"n"`
	Watermark        *bool  `json:"watermark"`
	Seed             *int64 `json:"seed"`
	ThinkingMode     *bool  `json:"thinkingMode"`
	EnableSequential *bool  `json:"enableSequential"`
	PromptExtend     *bool  `json:"promptExtend"`
}

// ImageGenerationHandler wires POST /api/generate/image to
// service.ImageGenerationService. It does binding/validation/delegation
// only — every actual business rule (catalog checks, credentials, upstream
// call, persistence) lives in the service.
type ImageGenerationHandler struct {
	images *service.ImageGenerationService
}

func NewImageGenerationHandler(images *service.ImageGenerationService) *ImageGenerationHandler {
	return &ImageGenerationHandler{images: images}
}

// Generate handles POST /api/generate/image. userID comes exclusively from
// the authenticated context (middleware.UserIDFrom) — never from the
// request body, so a client can never generate on someone else's behalf by
// forging a userID field.
func (h *ImageGenerationHandler) Generate(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	var body generateImageRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}

	task, err := h.images.GenerateImage(c.Request.Context(), userID, service.GenerateImageRequest{
		Model:  body.Model,
		Prompt: body.Prompt,
		Images: body.Images,
		Params: provider.ImageParams{
			Size:             body.Size,
			NegativePrompt:   body.NegativePrompt,
			N:                body.N,
			Watermark:        body.Watermark,
			Seed:             body.Seed,
			ThinkingMode:     body.ThinkingMode,
			EnableSequential: body.EnableSequential,
			PromptExtend:     body.PromptExtend,
		},
	})
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	c.JSON(http.StatusOK, common.OK(generationmodel.FromEntity(*task)))
}
