//go:build wireinject

package internal

import (
	"context"
	"encoding/base64"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chenhao/omnigen-ai/server/internal/config"
	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
	"github.com/chenhao/omnigen-ai/server/internal/router"
	"github.com/chenhao/omnigen-ai/server/internal/service"
	"github.com/chenhao/omnigen-ai/server/internal/worker"
)

// App 是装配完成的应用，暴露给 main 使用。
type App struct {
	Engine *gin.Engine
	Pool   *pgxpool.Pool
	Users  *service.UserService
	Config *config.Config
	// Worker 是后台轮询任务的 Poller，main.go 负责在装配完成、开始对外
	// 服务之前 go Worker.Run(ctx)，并在优雅停机时先取消它的 ctx、等它
	// 退出，再关 HTTP server——这样一次正在进行中的轮询不会被进程退出
	// 腰斩。
	Worker *worker.Poller
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

// provideSettingReader 把 *service.SettingService 收窄成
// service.SettingReader 接口——供图片/视频生成 service、上传 service、
// worker.Poller 依赖，它们只需要 GetDecrypted，不需要 SettingService 完整
// 的加解密/脱敏/连通性测试能力。与 provideDB 把 *pgxpool.Pool 收窄成
// repository.DB 是同一种 wire 手法。
func provideSettingReader(s *service.SettingService) service.SettingReader { return s }

// provideVideoProviderFactory 把生产环境的视频 provider 工厂交给 wire，
// 供 worker.Poller 使用。之所以是工厂而不是预先构造好的 provider 实例，
// 是因为 API Key/endpoint 来自 app_settings，管理员可能在不重启进程的
// 情况下修改它们——worker 每一轮轮询都用当时的配置重新构造 provider，
// 这条约束在 worker.New 的签名里已经体现，这里只是把生产实现接进 wire。
//
// image 侧没有对应的 provideImageProviderFactory：
// service.NewImageGenerationService（2 参数构造器）内部已经直接绑定生产
// 工厂 NewDashScopeT8starImageProviderFactory，wire 只需要 settings/tasks
// 两个依赖就能满足它，额外声明一个从未被图里任何构造器实际消费的工厂
// provider 只会是死代码——wire 不会报错（未使用的 provider 不算错误），
// 但会让人误以为这个工厂真的被注入了。VideoGenerationService 同理也用
// 自己内部的默认工厂，不经过这里；这里的工厂只服务于 worker 一处。
func provideVideoProviderFactory() service.VideoProviderFactory {
	return service.NewDashScopeVideoProviderFactory()
}

// provideWorker 包一层 worker.New：wire 无法直接给一个末尾是 `...Option`
// 变参的构造函数做静态分析（它不知道要传几个、传什么 Option），所以这里
// 用一个不带变参的薄包装把生产环境"零个 Option、用全部默认值"这个决定
// 显式表达出来。
func provideWorker(tasks repository.TaskRepository, settings service.SettingReader, factory service.VideoProviderFactory, archiver service.ResultArchiver) *worker.Poller {
	return worker.New(tasks, settings, factory, archiver)
}

func provideHandlers(
	a *handler.AuthHandler,
	u *handler.UserHandler,
	h *handler.HealthHandler,
	s *handler.SettingHandler,
	cat *handler.CatalogHandler,
	up *handler.UploadHandler,
	img *handler.ImageGenerationHandler,
	vid *handler.VideoGenerationHandler,
	dl *handler.DownloadHandler,
	opt *handler.OptimizeHandler,
	st *handler.StatsHandler,
) router.Handlers {
	return router.Handlers{
		Auth:            a,
		User:            u,
		Health:          h,
		Setting:         s,
		Catalog:         cat,
		Upload:          up,
		ImageGeneration: img,
		VideoGeneration: vid,
		Download:        dl,
		Optimize:        opt,
		Stats:           st,
	}
}

func provideCORSOrigins(cfg *config.Config) []string {
	return cfg.CORSOrigins
}

// provideEncryptionKey 把 cfg.AppEncryptionKey（base64 字符串）解码成
// SettingService 直接可用的 32 字节原始密钥。cfg.AppEncryptionKey 已经在
// config.Load 里校验过是合法 base64 且解码后恰好 32 字节，这里的 err 理论上
// 不可能非 nil；仍然照常返回而不是 panic/忽略，是因为"理论上不会发生"不等于
// "允许在类型层面假装不会失败"——万一 Config 的构造路径将来出现绕过 Load()
// 校验的第二条来源，这里也不会把一个畸形密钥悄悄传给 crypto 包。
func provideEncryptionKey(cfg *config.Config) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(cfg.AppEncryptionKey)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func provideApp(e *gin.Engine, p *pgxpool.Pool, u *service.UserService, cfg *config.Config, w *worker.Poller) *App {
	return &App{Engine: e, Pool: p, Users: u, Config: cfg, Worker: w}
}

var providerSet = wire.NewSet(
	providePool,
	provideDB,
	providePinger,
	provideJWT,
	provideCORSOrigins,
	provideEncryptionKey,
	provideSettingReader,
	provideVideoProviderFactory,
	service.NewResultArchiver,
	// NewResultArchiver 返回具体类型 *ResultArchiveService，而图片 service 与
	// worker 依赖的是 ResultArchiver 接口。wire 不会自己把实现「看成」接口，
	// 必须显式声明这条绑定，否则图里缺少 ResultArchiver 的 provider。
	// 与 provideDB / provideSettingReader 用薄函数收窄类型是同一件事的两种
	// 写法，这里用 Bind 是因为不需要额外的构造逻辑。
	wire.Bind(new(service.ResultArchiver), new(*service.ResultArchiveService)),
	repository.NewUserRepository,
	repository.NewSettingRepository,
	repository.NewTaskRepository,
	repository.NewStatsRepository,
	service.NewAuthService,
	service.NewUserService,
	service.NewSettingService,
	service.NewUploadService,
	service.NewQuotaService,
	service.NewImageGenerationService,
	service.NewVideoGenerationService,
	service.NewOptimizeService,
	service.NewStatsService,
	handler.NewAuthHandler,
	handler.NewUserHandler,
	handler.NewHealthHandler,
	handler.NewSettingHandler,
	handler.NewCatalogHandler,
	handler.NewUploadHandler,
	handler.NewImageGenerationHandler,
	handler.NewVideoGenerationHandler,
	handler.NewDownloadHandler,
	handler.NewOptimizeHandler,
	handler.NewStatsHandler,
	provideHandlers,
	provideWorker,
	router.New,
	provideApp,
)

func InitApp(ctx context.Context, cfg *config.Config) (*App, error) {
	wire.Build(providerSet)
	return nil, nil
}
