package router

import (
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

type Handlers struct {
	Auth            *handler.AuthHandler
	User            *handler.UserHandler
	Health          *handler.HealthHandler
	Setting         *handler.SettingHandler
	Catalog         *handler.CatalogHandler
	Upload          *handler.UploadHandler
	ImageGeneration *handler.ImageGenerationHandler
	VideoGeneration *handler.VideoGenerationHandler
	Download        *handler.DownloadHandler
	Optimize        *handler.OptimizeHandler
	Stats           *handler.StatsHandler
}

func New(h Handlers, jwtMgr *jwtx.Manager, users repository.UserRepository, corsOrigins []string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), middleware.Recovery(), middleware.ErrorHandler())

	// 开发期允许 Vite dev server 跨域；生产由同源部署或反向代理承担。
	// 允许的 origin 列表来自 config.Config.CORSOrigins（config.yaml 的
	// cors.origins），而非写死在这里——见 config.defaultCORSOrigins 的注释。
	corsCfg := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}
	if hasWildcard(corsOrigins) {
		// 放行全部来源时**不能**把 "*" 塞进 AllowOrigins。
		//
		// CORS 规范禁止 `Access-Control-Allow-Origin: *` 与
		// `Access-Control-Allow-Credentials: true` 同时出现，浏览器会直接
		// 拒绝整个响应。而 gin-contrib/cors 在 AllowOrigins 含 "*" 时正是
		// 回字面量 "*"，配上这里的 AllowCredentials 就构成那个非法组合——
		// 症状是跨域请求在浏览器侧全部失败，服务端日志却一切正常，
		// 极难定位（router_test.go 里有一条专门钉住这一点）。
		//
		// 用 AllowOriginFunc 恒真代替：库会把请求自带的 Origin 原样回显，
		// 效果同样是"谁都放行"，但和 AllowCredentials 不冲突。
		corsCfg.AllowOriginFunc = func(string) bool { return true }
	} else {
		corsCfg.AllowOrigins = corsOrigins
	}
	r.Use(cors.New(corsCfg))

	api := r.Group("/api")
	api.GET("/health", h.Health.Check)
	api.POST("/auth/login", h.Auth.Login)

	authed := api.Group("", middleware.Auth(jwtMgr, users))
	authed.GET("/auth/me", h.Auth.Me)
	authed.POST("/auth/logout", h.Auth.Logout)
	authed.PUT("/auth/password", h.Auth.ChangePassword)
	authed.GET("/settings", h.Setting.Get)
	authed.GET("/catalog", h.Catalog.Get)
	authed.POST("/upload", h.Upload.Upload)
	authed.POST("/generate/image", h.ImageGeneration.Generate)
	authed.POST("/generate/video", h.VideoGeneration.Generate)
	authed.POST("/optimize-prompt", h.Optimize.Optimize)
	authed.GET("/tasks", h.VideoGeneration.List)
	authed.GET("/tasks/:id", h.VideoGeneration.Get)
	authed.DELETE("/tasks/:id", h.VideoGeneration.Delete)
	authed.DELETE("/tasks", h.VideoGeneration.DeleteAll)
	authed.GET("/download/:taskId/:index", h.Download.Download)
	authed.GET("/stats", h.Stats.Get)

	admin := authed.Group("", middleware.RequireAdmin())
	admin.GET("/users", h.User.List)
	admin.POST("/users", h.User.Create)
	admin.PUT("/users/:id", h.User.Update)
	admin.PUT("/users/:id/password", h.User.ResetPassword)
	admin.DELETE("/users/:id", h.User.Delete)
	admin.PUT("/settings", h.Setting.Update)
	admin.POST("/settings/test", h.Setting.TestConnection)

	return r
}

// hasWildcard 判断 cors.origins 是否要求放行全部来源。
//
// 只认独占一项的 "*"：写成 "*.example.com" 这类子域通配不在支持范围内
// （gin-contrib/cors 本身也不做子域匹配），把它当成全放行会大幅超出配置者
// 的本意。
func hasWildcard(origins []string) bool {
	for _, o := range origins {
		if strings.TrimSpace(o) == "*" {
			return true
		}
	}
	return false
}
