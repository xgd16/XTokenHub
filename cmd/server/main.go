// XTokenHub 服务入口：配置 -> 数据库 -> 事件总线/WS Hub -> 路由 -> 优雅退出。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/cleanup"
	"xtokenhub/internal/config"
	"xtokenhub/internal/database"
	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/handler/admin"
	gwhandler "xtokenhub/internal/handler/gateway"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/router"
	"xtokenhub/internal/service"
	"xtokenhub/internal/ws"
	"xtokenhub/web"
)

// 构建期由 ldflags 注入：go build -ldflags "-X main.version=... -X main.commit=..."
var (
	version = "dev"
	commit  = "none"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	showVersion := flag.Bool("version", false, "打印版本信息并退出")
	flag.Parse()
	if *showVersion {
		fmt.Printf("xtokenhub %s (commit %s)\n", version, commit)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}
	logger.Init(logger.Options{Level: logger.Level(cfg.Log.Level), Format: cfg.Log.Format})
	log := logger.L("main")

	gin.SetMode(ginMode(cfg.Server.Mode))

	// 数据库
	db, err := database.Open(&cfg.Database)
	if err != nil {
		log.Error("初始化数据库失败", logger.Err(err))
		os.Exit(1)
	}
	if err := database.Migrate(db); err != nil {
		log.Error("数据库迁移失败", logger.Err(err))
		os.Exit(1)
	}

	// 事件总线 + WS Hub
	bus := eventbus.New()
	hub := ws.NewHub(
		time.Duration(cfg.WS.PingInterval)*time.Second,
		time.Duration(cfg.WS.WriteTimeout)*time.Second,
	)
	hub.AttachBus(bus,
		eventbus.EventRequestStarted,
		eventbus.EventRequestCompleted,
		eventbus.EventStatsUpdated,
		eventbus.EventThroughput,
		eventbus.EventChannelProbeResult,
		eventbus.EventChannelStatusChange,
		eventbus.EventChannelBalance,
	)

	// 仓储与服务
	chRepo := repository.NewChannelRepository(db)
	keyRepo := repository.NewAPIKeyRepository(db)
	logRepo := repository.NewRequestLogRepository(db)
	customRepo := repository.NewCustomModelRepository(db)
	sessionHeaderRepo := repository.NewSessionHeaderConfigRepo(db)
	prober := provider.NewProber(time.Duration(cfg.Gateway.UpstreamTimeout) * time.Second)

	channelSvc := service.NewChannelService(chRepo, prober, bus)
	// 渠道余额查询（当前支持 DeepSeek，按 BaseURL host 识别）
	channelSvc.SetBalanceClient(provider.NewBalanceClient(15 * time.Second))
	keySvc := service.NewKeyService(keyRepo)
	logSvc := service.NewLogService(logRepo)
	statsSvc := service.NewStatsService(logRepo)
	modelSvc := service.NewModelService(logRepo, chRepo, customRepo)
	customSvc := service.NewCustomModelService(customRepo)
	sessionHeaderSvc := service.NewSessionHeaderConfigService(sessionHeaderRepo)
	// 种子化默认会话标识（X-Session-Id）：表为空时写入，保证升级后行为不变
	if err := sessionHeaderSvc.EnsureDefaults(context.Background()); err != nil {
		log.Warn("种子化会话标识配置失败", logger.Err(err))
	}
	exec := gateway.NewExecutor(chRepo, logRepo, bus, time.Duration(cfg.Gateway.UpstreamTimeout)*time.Second)
	exec.SetCustomModels(customRepo)
	throughput := gateway.NewThroughput(bus, 500*time.Millisecond, 5*time.Second)
	exec.SetThroughput(throughput)
	thCtx, thCancel := context.WithCancel(context.Background())
	go throughput.Run(thCtx)

	// 日志保留期清理（默认开启；手动 API 始终可用）
	retention := cleanup.NewRetention(logRepo, db, cleanup.Options{
		Enabled:     cfg.Retention.Enabled,
		MaxDays:     cfg.Retention.MaxDays,
		IntervalHrs: cfg.Retention.IntervalHours,
		BatchSize:   cfg.Retention.BatchSize,
		Vacuum:      cfg.Retention.Vacuum,
	})
	retSvc := service.NewRetentionService(retention)
	retCtx, retCancel := context.WithCancel(context.Background())
	if cfg.Retention.Enabled {
		go retention.Run(retCtx)
	}

	// 路由
	engine := gin.New()
	deps := &router.Deps{
		Cfg:            cfg,
		Channels:       admin.NewChannelHandler(channelSvc),
		CustomModels:   admin.NewCustomModelHandler(customSvc),
		Models:         admin.NewModelHandler(modelSvc),
		Keys:           admin.NewKeyHandler(keySvc),
		Logs:           admin.NewLogHandler(logSvc),
		Stats:          admin.NewStatsHandler(statsSvc),
		Cleanup:        admin.NewCleanupHandler(retSvc),
		WS:             admin.NewWSHandler(hub),
		SessionHeaders: admin.NewSessionHeaderConfigHandler(sessionHeaderSvc),
		Gateway:        gwhandler.NewHandler(exec, keySvc, cfg.Gateway.RequireKey, cfg.Gateway.MaxBodyBytes),
	}
	// 网关按配置的会话 Header 名单提取会话标识（默认 X-Session-Id + 自定义扩展）
	deps.Gateway.SetSessionHeaders(sessionHeaderSvc)
	if web.Ready() {
		deps.WebFS = web.FS()
		log.Info("前端产物已内嵌，SPA 托管启用")
	} else {
		log.Warn("未发现前端产物 (make web)，仅提供 API")
	}
	router.Register(engine, deps)

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: engine,
	}

	// 优雅退出
	go func() {
		log.Info("服务启动", "addr", cfg.Addr(), "mode", cfg.Server.Mode, "require_key", cfg.Gateway.RequireKey)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP 服务异常", logger.Err(err))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("收到退出信号，开始优雅关闭")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("HTTP 关闭超时", logger.Err(err))
	}
	thCancel()
	retCancel()
	hub.Close()
	bus.Wait()
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	log.Info("服务已退出")
	slog.Default()
}

func ginMode(mode string) string {
	switch mode {
	case "release", "test":
		return mode
	default:
		return "debug"
	}
}
