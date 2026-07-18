package router

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

type Handlers struct {
	Auth   *handler.AuthHandler
	User   *handler.UserHandler
	Health *handler.HealthHandler
}

func New(h Handlers, jwtMgr *jwtx.Manager, users repository.UserRepository, corsOrigins []string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), middleware.Recovery(), middleware.ErrorHandler())

	// 开发期允许 Vite dev server 跨域；生产由同源部署或反向代理承担。
	// 允许的 origin 列表来自 config.Config.CORSOrigins（CORS_ORIGINS 环境变量），
	// 而非写死在这里——见 config.defaultCORSOrigins 的注释。
	r.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	api := r.Group("/api")
	api.GET("/health", h.Health.Check)
	api.POST("/auth/login", h.Auth.Login)

	authed := api.Group("", middleware.Auth(jwtMgr, users))
	authed.GET("/auth/me", h.Auth.Me)
	authed.POST("/auth/logout", h.Auth.Logout)
	authed.PUT("/auth/password", h.Auth.ChangePassword)

	admin := authed.Group("", middleware.RequireAdmin())
	admin.GET("/users", h.User.List)
	admin.POST("/users", h.User.Create)
	admin.PUT("/users/:id", h.User.Update)
	admin.PUT("/users/:id/password", h.User.ResetPassword)
	admin.DELETE("/users/:id", h.User.Delete)

	return r
}
