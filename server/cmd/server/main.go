package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chenhao/omnigen-ai/server/internal"
	"github.com/chenhao/omnigen-ai/server/internal/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := run(); err != nil {
		slog.Error("服务启动失败", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	configPath := flag.String("config", "config.yaml", "配置文件路径（YAML）")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	app, err := internal.InitApp(ctx, cfg)
	if err != nil {
		return fmt.Errorf("装配应用失败: %w", err)
	}
	defer app.Pool.Close()

	// 首启播种：无任何活跃 admin 时才创建，已存在则跳过、不覆盖。
	if err := app.Users.EnsureBootstrapAdmin(ctx, cfg.Bootstrap.Username, cfg.Bootstrap.Password); err != nil {
		return fmt.Errorf("播种管理员失败: %w", err)
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           app.Engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// serveErr 是 ListenAndServe 失败（端口被占用等）的唯一出口。以前这里只
	// 在 goroutine 内 slog.Error 就完事，没有任何东西让主 goroutine 感知到
	// 失败——进程会一直卡在 <-quit 上，端口没监听却"活着"。现在监听失败会
	// 送进这个 channel，下面的 select 会把它当成退出条件，最终让 run()
	// 返回非 nil、main() 里的 os.Exit(1) 生效。缓冲区为 1 是为了正常关闭
	// 路径（srv.Shutdown 触发 ListenAndServe 返回 http.ErrServerClosed）也
	// 能往里塞一个值而不阻塞 goroutine——那个值此后不会再被读取，但 send
	// 本身必须不阻塞，否则这个 goroutine 就泄漏了。
	serveErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP 服务已启动", "port", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	// 轮询 worker 与 HTTP 服务的生命周期绑在一起启动、绑在一起停止：
	// worker 用独立的 workerCtx（而非直接用 ctx）是因为 ctx 是
	// context.Background() 派生的"永不取消"的根，workerCtx 才是真正在收到
	// 退出信号时会被 cancel 的那个。workerDone 在 Worker.Run 返回后关闭——
	// 无论是 quit 分支还是 serveErr 分支，run() 在返回前都会等
	// <-workerDone，保证进程不会在一轮轮询正在进行时退出。
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		app.Worker.Run(workerCtx)
	}()
	stopWorker := func() {
		slog.Info("正在停止轮询 worker…")
		cancelWorker()
		<-workerDone
		slog.Info("轮询 worker 已停止")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("收到退出信号，正在关闭服务…", "signal", sig.String())
	case err := <-serveErr:
		// 监听从一开始就没起来（或异常退出）——没有什么可以优雅关闭的，
		// 停掉 worker 后直接把错误返回给 main()，由它以非零状态码退出。
		stopWorker()
		return fmt.Errorf("HTTP 服务监听失败: %w", err)
	}

	// Worker 必须先于 HTTP 优雅关闭完成之前停下——这里让它在
	// srv.Shutdown 开始之前就已经完全停止，天然满足"先于关闭完成"这个
	// 更宽松的要求。
	stopWorker()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("关闭 HTTP 服务失败: %w", err)
	}
	return nil
}
