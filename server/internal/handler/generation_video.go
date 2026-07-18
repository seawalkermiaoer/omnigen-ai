package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// videoMediaImageBody / videoMediaVideoBody are the r2v reference media
// entries as received over the wire — mirror service.R2VMediaImage/
// R2VMediaVideo, this is the one place that maps JSON field names to them.
type videoMediaImageBody struct {
	URL            string `json:"url" binding:"required"`
	ReferenceVoice string `json:"referenceVoice"`
}

type videoMediaVideoBody struct {
	URL            string `json:"url" binding:"required"`
	ReferenceVoice string `json:"referenceVoice"`
}

// createVideoTaskRequestBody is the POST /api/generate/video JSON body.
// Which fields matter depends on the model's catalog capability (r2v vs
// i2v vs t2v) — same "fields are only consulted for the resolved mode"
// contract as service.CreateVideoTaskRequest; the handler does no mode
// branching of its own, it just forwards everything to the service.
type createVideoTaskRequestBody struct {
	Model  string `json:"model" binding:"required"`
	Prompt string `json:"prompt"`

	Images []videoMediaImageBody `json:"images"`
	Videos []videoMediaVideoBody `json:"videos"`

	TaskType     string `json:"taskType"`
	FirstFrame   string `json:"firstFrame"`
	LastFrame    string `json:"lastFrame"`
	FirstClip    string `json:"firstClip"`
	DrivingAudio string `json:"drivingAudio"`

	NegativePrompt string `json:"negativePrompt"`
	PromptExtend   *bool  `json:"promptExtend"`
	Seed           *int64 `json:"seed"`

	// Resolution/Duration/Watermark are never optional on the wire either —
	// an omitted field decodes to its Go zero value ("", 0, false), and
	// service.normalizeVideoParams treats "" / 0 as "use the old UI's
	// default" (720P / 5s) exactly the way an untouched <select>/<input>
	// would have. Ratio is optional-and-mode-gated: sending it on an i2v
	// request is a validation error, not silently ignored — see
	// service.normalizeVideoParams's doc for why.
	Resolution string `json:"resolution"`
	Duration   int    `json:"duration"`
	Ratio      string `json:"ratio"`
	Watermark  bool   `json:"watermark"`
}

func (b createVideoTaskRequestBody) toServiceRequest() service.CreateVideoTaskRequest {
	images := make([]service.R2VMediaImage, 0, len(b.Images))
	for _, img := range b.Images {
		images = append(images, service.R2VMediaImage{URL: img.URL, ReferenceVoice: img.ReferenceVoice})
	}
	videos := make([]service.R2VMediaVideo, 0, len(b.Videos))
	for _, v := range b.Videos {
		videos = append(videos, service.R2VMediaVideo{URL: v.URL, ReferenceVoice: v.ReferenceVoice})
	}

	return service.CreateVideoTaskRequest{
		Model:  b.Model,
		Prompt: b.Prompt,

		Images: images,
		Videos: videos,

		TaskType:     service.I2VTaskType(b.TaskType),
		FirstFrame:   b.FirstFrame,
		LastFrame:    b.LastFrame,
		FirstClip:    b.FirstClip,
		DrivingAudio: b.DrivingAudio,

		Params: service.VideoParams{
			NegativePrompt: b.NegativePrompt,
			PromptExtend:   b.PromptExtend,
			Seed:           b.Seed,
			Resolution:     b.Resolution,
			Duration:       b.Duration,
			Ratio:          b.Ratio,
			Watermark:      b.Watermark,
		},
	}
}

// VideoGenerationHandler wires POST /api/generate/video, GET /api/tasks/:id
// and GET /api/tasks to service.VideoGenerationService. Like
// ImageGenerationHandler, it only does binding/validation/delegation —
// every business rule (catalog checks, credentials, upstream call,
// ownership scoping) lives in the service.
type VideoGenerationHandler struct {
	videos *service.VideoGenerationService
}

func NewVideoGenerationHandler(videos *service.VideoGenerationService) *VideoGenerationHandler {
	return &VideoGenerationHandler{videos: videos}
}

// Generate handles POST /api/generate/video. userID comes exclusively from
// the authenticated context, never from the request body — same reasoning
// as ImageGenerationHandler.Generate.
func (h *VideoGenerationHandler) Generate(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	var body createVideoTaskRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}

	task, err := h.videos.CreateVideoTask(c.Request.Context(), userID, body.toServiceRequest())
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	c.JSON(http.StatusOK, common.OK(generationmodel.FromEntity(*task)))
}

// Get handles GET /api/tasks/:id, scoped to the authenticated user —
// another user's task (or a nonexistent id) both surface as
// apperr.ErrTaskNotFound, never a 403.
func (h *VideoGenerationHandler) Get(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	id, err := pathID(c)
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	task, err := h.videos.GetTask(c.Request.Context(), userID, id)
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	c.JSON(http.StatusOK, common.OK(generationmodel.FromEntity(*task)))
}

// List handles GET /api/tasks, scoped to the authenticated user's own tasks.
func (h *VideoGenerationHandler) List(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	var q generationmodel.ListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}

	items, total, err := h.videos.ListTasks(c.Request.Context(), userID, q)
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	c.JSON(http.StatusOK, common.OK(generationmodel.TaskListResponse{
		Total: total,
		Items: generationmodel.FromEntities(items),
	}))
}

// Delete handles DELETE /api/tasks/:id, scoped to the authenticated user —
// same not-found-vs-forbidden contract as Get: another user's task (or a
// nonexistent id) both surface as apperr.ErrTaskNotFound. Deletion is
// allowed regardless of task status, including in-flight PENDING/RUNNING
// tasks — see service.VideoGenerationService.DeleteTask's doc for why.
func (h *VideoGenerationHandler) Delete(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	id, err := pathID(c)
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	if err := h.videos.DeleteTask(c.Request.Context(), userID, id); err != nil {
		middleware.Fail(c, err)
		return
	}

	c.JSON(http.StatusOK, common.OK(nil))
}

// DeleteAll handles DELETE /api/tasks, clearing only the authenticated
// user's own tasks — never another user's, regardless of role. Returns the
// deleted count so the UI can confirm "清空全部" actually happened.
func (h *VideoGenerationHandler) DeleteAll(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	deleted, err := h.videos.DeleteAllTasks(c.Request.Context(), userID)
	if err != nil {
		middleware.Fail(c, err)
		return
	}

	c.JSON(http.StatusOK, common.OK(generationmodel.TaskDeleteAllResponse{Deleted: deleted}))
}
