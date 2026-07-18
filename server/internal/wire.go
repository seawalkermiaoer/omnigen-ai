//go:build wireinject

package internal

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chenhao/omnigen-ai/server/internal/config"
	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/router"
	"github.com/chenhao/omnigen-ai/server/internal/service"
)

// App 是装配完成的应用，暴露给 main 使用。
type App struct {
	Engine *gin.Engine
	Pool   *pgxpool.Pool
	Users  *service.UserService
	Config *config.Config
}

func provideJWT(cfg *config.Config) *jwtx.Manager {
	return jwtx.NewManager(cfg.JWT.Secret, cfg.JWT.TTL)
}

func providePool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	return repository.NewPool(ctx, cfg.DB.DSN())
}

// provideDB 把连接池收窄成 repository.DB 接口，
// 使 repository 的实现对「池」还是「事务」无感。
func provideDB(pool *pgxpool.Pool) repository.DB { return pool }

func providePinger(pool *pgxpool.Pool) handler.Pinger { return pool }

func provideHandlers(a *handler.AuthHandler, u *handler.UserHandler, h *handler.HealthHandler) router.Handlers {
	return router.Handlers{Auth: a, User: u, Health: h}
}

func provideApp(e *gin.Engine, p *pgxpool.Pool, u *service.UserService, cfg *config.Config) *App {
	return &App{Engine: e, Pool: p, Users: u, Config: cfg}
}

var providerSet = wire.NewSet(
	providePool,
	provideDB,
	providePinger,
	provideJWT,
	repository.NewUserRepository,
	service.NewAuthService,
	service.NewUserService,
	handler.NewAuthHandler,
	handler.NewUserHandler,
	handler.NewHealthHandler,
	provideHandlers,
	router.New,
	provideApp,
)

func InitApp(ctx context.Context, cfg *config.Config) (*App, error) {
	wire.Build(providerSet)
	return nil, nil
}
