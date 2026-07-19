package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	statsmodel "github.com/chenhao/omnigen-ai/server/internal/model/stats"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// statsQueryBody is the GET /api/stats query string. From/To are plain
// strings (not time.Time) so a malformed value can be turned into
// apperr.ErrValidation ourselves — binding a bare time.Time field would let
// gin's default time format silently reject valid RFC3339 timestamps or
// accept a format we don't intend to support.
//
// UserID is *int64 so "absent" (nil) is distinguishable from "present" —
// service.StatsService.GetReport relies on that distinction: nil means "no
// filter" for an admin, and is what a non-admin's value gets forced to
// regardless of what they sent.
type statsQueryBody struct {
	From   string `form:"from"`
	To     string `form:"to"`
	UserID *int64 `form:"userId"`
}

// StatsHandler wires GET /api/stats to service.StatsService. Like the other
// handlers, it only does binding/validation/delegation — the permission
// narrowing (non-admin can't see another user's data) lives entirely in the
// service, never here.
type StatsHandler struct {
	stats *service.StatsService
}

func NewStatsHandler(stats *service.StatsService) *StatsHandler {
	return &StatsHandler{stats: stats}
}

// Get handles GET /api/stats?from=&to=&userId=, behind the logged-in
// middleware. actorID/actorRole come exclusively from the authenticated
// context, never from the request — the whole point of this endpoint's
// permission model is that a client can't talk its way into someone else's
// data by passing a different userId.
func (h *StatsHandler) Get(c *gin.Context) {
	actorID, ok := middleware.UserIDFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}
	actorRole, ok := middleware.RoleFrom(c)
	if !ok {
		middleware.Fail(c, apperr.ErrUnauthorized)
		return
	}

	var body statsQueryBody
	if err := c.ShouldBindQuery(&body); err != nil {
		middleware.Fail(c, apperr.ErrValidation.Wrap(err))
		return
	}

	q := statsmodel.Query{UserID: body.UserID}
	if body.From != "" {
		from, err := time.Parse(time.RFC3339, body.From)
		if err != nil {
			middleware.Fail(c, apperr.ErrValidation.Wrap(err))
			return
		}
		q.From = &from
	}
	if body.To != "" {
		to, err := time.Parse(time.RFC3339, body.To)
		if err != nil {
			middleware.Fail(c, apperr.ErrValidation.Wrap(err))
			return
		}
		q.To = &to
	}

	report, err := h.stats.GetReport(c.Request.Context(), actorID, actorRole, q)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, common.OK(report))
}
