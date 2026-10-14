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

	"xtokenhub/internal/aggregation"
	"xtokenhub/internal/cleanup"
	"xtokenhub/internal/config"
	"xtokenhub/internal/database"
	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/handler/admin"
	gwhandler "xtokenhub/internal/handler/gateway"
	mcp "xtokenhub/internal/handler/mcp"
	memoryhandler "xtokenhub/internal/handler/memory"
	"xtokenhub/internal/model"
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
	priceRepo := repository.NewModelPriceRepository(db)
	billingRepo := repository.NewBillingSettingsRepository(db)
	dailyRepo := repository.NewDailyStatsRepository(db)
	dataRepo := repository.NewDataSettingsRepository(db)
	mimoRepo := repository.NewMimoSettingsRepository(db)
	prober := provider.NewProber(time.Duration(cfg.Gateway.UpstreamTimeout) * time.Second)

	channelSvc := service.NewChannelService(chRepo, prober, bus)
	// 渠道余额查询（DeepSeek 走 API Key；MiMo 走网页 Cookie，端点在平台域名）
	channelSvc.SetBalanceClient(provider.NewBalanceClient(15 * time.Second))
	mimoSvc := service.NewMimoSettingsService(mimoRepo)
	channelSvc.SetBalanceCookieProvider(mimoSvc.Cookie)
	if err := mimoSvc.EnsureDefaults(context.Background()); err != nil {
		log.Warn("播种 MiMo 凭证设置失败", logger.Err(err))
	}
	// 保存新 Cookie 后立即失效余额缓存，下次查询即用新凭证
	mimoSvc.SetOnCookieChanged(channelSvc.InvalidateBalances)
	keySvc := service.NewKeyService(keyRepo)
	logSvc := service.NewLogService(logRepo)
	statsSvc := service.NewStatsService(logRepo, dailyRepo)
	modelSvc := service.NewModelService(logRepo, chRepo, customRepo)
	modelSvc.SetDailyStats(dailyRepo)
	customSvc := service.NewCustomModelService(customRepo)
	sessionHeaderSvc := service.NewSessionHeaderConfigService(sessionHeaderRepo)
	// 种子化默认会话标识（X-Session-Id）：表为空时写入，保证升级后行为不变
	if err := sessionHeaderSvc.EnsureDefaults(context.Background()); err != nil {
		log.Warn("种子化会话标识配置失败", logger.Err(err))
	}

	// 节假日年历：峰谷规则 !H / W 记号的判定依据（DeepSeek 等厂商节假日全天谷价）
	holidayRepo := repository.NewHolidayDateRepository(db)
	holidaySvc := service.NewHolidayService(holidayRepo,
		provider.NewHolidayClient(time.Duration(cfg.Holiday.TimeoutSeconds)*time.Second),
		service.HolidayOptions{
			Enabled:      cfg.Holiday.Enabled,
			AutoSync:     cfg.Holiday.AutoSync,
			SyncInterval: time.Duration(cfg.Holiday.SyncIntervalHours) * time.Hour,
			SourceURL:    cfg.Holiday.SourceURL,
		})
	model.SetHolidaySource(holidaySvc)
	if err := holidaySvc.LoadCache(context.Background()); err != nil {
		log.Warn("载入节假日年历失败", logger.Err(err))
	}

	// 计价：价格表 + 请求级费用 + 花费预测（价格表为空时首次启动自动同步一次）
	costSvc := service.NewCostService(
		priceRepo, logRepo, billingRepo,
		provider.NewPricingClient(time.Duration(cfg.Pricing.TimeoutSeconds)*time.Second),
		service.CostOptions{
			Enabled:      cfg.Pricing.Enabled,
			AutoSync:     cfg.Pricing.AutoSync,
			SyncInterval: time.Duration(cfg.Pricing.SyncIntervalHours) * time.Hour,
			SourceURL:    cfg.Pricing.SourceURL,
			// 仅首次初始化入库，之后以设置页的值为准
			InitialBilling: model.BillingSettings{
				DisplayCurrency:  cfg.Billing.DisplayCurrency,
				USDRate:          cfg.Billing.USDRate,
				MonthlyBudgetUSD: cfg.Billing.MonthlyBudgetUSD,
			},
		},
	)
	costSvc.SetChannels(chRepo)
	costSvc.SetDailyStats(dailyRepo)
	if err := costSvc.EnsureDefaults(context.Background()); err != nil {
		log.Warn("初始化计价服务失败", logger.Err(err))
	}
	priceCtx, priceCancel := context.WithCancel(context.Background())
	go costSvc.RunAutoSync(priceCtx)
	go holidaySvc.RunAutoSync(priceCtx)
	// 年历首次启动同步一次（表为空时），之后由定时同步跟进新公布的安排
	go func() {
		if n, err := holidayRepo.Count(priceCtx); err == nil && n == 0 {
			if _, err := holidaySvc.Sync(priceCtx); err != nil {
				log.Warn("首次同步节假日年历失败", logger.Err(err))
			}
		}
	}()
	// 存量日志补算：补上启用计价之前的历史请求费用（幂等，无变化不写库）
	go func() {
		if _, err := costSvc.Recompute(priceCtx, time.Time{}, time.Time{}, true); err != nil {
			log.Warn("存量日志费用补算失败", logger.Err(err))
		}
	}()

	exec := gateway.NewExecutor(chRepo, logRepo, bus, time.Duration(cfg.Gateway.UpstreamTimeout)*time.Second)
	exec.SetCustomModels(customRepo)
	exec.SetPricing(costSvc)
	// 思维档位改写：按上游模型把 reasoning_effort 等档位翻译成 thinking 开关（MiMo 类上游）
	reasoningRepo := repository.NewReasoningRewriteRepository(db)
	reasoningSvc := service.NewReasoningRewriteService(reasoningRepo)
	exec.SetRewrites(reasoningSvc)
	throughput := gateway.NewThroughput(bus, 500*time.Millisecond, 5*time.Second)
	exec.SetThroughput(throughput)
	thCtx, thCancel := context.WithCancel(context.Background())
	go throughput.Run(thCtx)

	// 日志保留期清理 + 日级聚合：配置存 data_settings（首次以 yaml retention 播种，
	// 之后以设置页为准，改完即生效），执行结果持久化回设置行，重启不丢。
	aggSvc := aggregation.New(dailyRepo, dataRepo)
	cleanupSettings := func(ctx context.Context) (cleanup.Settings, error) {
		row, err := dataRepo.Get(ctx)
		if err != nil {
			return cleanup.Settings{}, err
		}
		return cleanup.Settings{
			Enabled:              row.CleanupEnabled,
			Mode:                 row.CleanupMode,
			RunHour:              row.CleanupRunHour,
			IntervalHours:        row.CleanupIntervalHours,
			MaxDays:              row.CleanupMaxDays,
			BatchSize:            row.CleanupBatchSize,
			Vacuum:               row.CleanupVacuum,
			TrimHeaders:          row.TrimHeaders,
			TrimHeadersAfterDays: row.TrimHeadersAfterDays,
			LastCleanupDay:       row.LastCleanupDay,
			LastCleanupAt:        row.LastCleanupAt,
		}, nil
	}
	persistResult := func(ctx context.Context, res *cleanup.Result) {
		if err := dataRepo.UpdateCleanupStatus(ctx, res.DoneAt, repository.DayBucketOf(res.DoneAt),
			res.DeletedRows, res.TrimmedRows, res.DurationMS, res.Error); err != nil {
			log.Warn("清理结果持久化失败", logger.Err(err))
		}
	}
	retention := cleanup.NewRetention(logRepo, db, cleanupSettings, persistResult)
	// 费用重算会改写明细 cost_usd，完成后把受影响日重建进聚合表
	costSvc.SetPostRecompute(func(ctx context.Context, from, to time.Time) {
		if err := aggSvc.RebuildRange(ctx, from, to); err != nil {
			log.Warn("费用重算后重建聚合失败", logger.Err(err))
		}
	})
	dataSvc := service.NewDataSettingsService(dataRepo, aggSvc, retention, model.DataSettings{
		CleanupEnabled:       cfg.Retention.Enabled,
		CleanupMode:          model.CleanupModeDaily,
		CleanupRunHour:       4, // 默认凌晨 4 点低峰执行
		CleanupIntervalHours: cfg.Retention.IntervalHours,
		CleanupMaxDays:       cfg.Retention.MaxDays,
		CleanupBatchSize:     cfg.Retention.BatchSize,
		CleanupVacuum:        cfg.Retention.Vacuum,
		TrimHeaders:          true,
		TrimHeadersAfterDays: 7,
	})
	if err := dataSvc.EnsureDefaults(context.Background()); err != nil {
		log.Warn("播种数据管理设置失败", logger.Err(err))
	}
	retSvc := service.NewRetentionService(retention)
	retCtx, retCancel := context.WithCancel(context.Background())
	go retention.Run(retCtx)
	aggCtx, aggCancel := context.WithCancel(context.Background())
	go aggSvc.Run(aggCtx)

	// 记忆库：Agent 共享记忆（FTS5 + 中文 n-gram 检索，无向量）+ 控制台管理 + LLM 一键分类
	memoryRepo := repository.NewMemoryRepository(db)
	memoryNsRepo := repository.NewMemoryNamespaceRepository(db)
	memoryLinkRepo := repository.NewMemoryLinkRepository(db)
	memorySettingsRepo := repository.NewMemorySettingsRepository(db)
	memorySvc := service.NewMemoryService(memoryRepo, memoryNsRepo, memoryLinkRepo, memorySettingsRepo,
		service.MemoryOptions{
			SearchLimitDefault: cfg.Memory.SearchLimitDefault,
			SearchLimitMax:     cfg.Memory.SearchLimitMax,
		})
	if err := memorySvc.EnsureDefaults(context.Background()); err != nil {
		log.Warn("播种记忆库设置失败", logger.Err(err))
	}
	memoryClassifier := service.NewMemoryClassifier(memoryRepo, memoryNsRepo, memorySettingsRepo, chRepo,
		provider.NewChannelClassifier(time.Duration(cfg.Memory.ClassifierTimeoutSeconds)*time.Second),
		service.MemoryClassifyOptions{BatchSize: cfg.Memory.ClassifierBatchSize})
	// 分类任务不跨重启续跑：把上轮卡在 running 的状态标记为失败，避免按钮永远「进行中」
	if err := memoryClassifier.ResetStale(context.Background()); err != nil {
		log.Warn("重置记忆分类任务状态失败", logger.Err(err))
	}
	memoryAgentHandler := memoryhandler.NewHandler(memorySvc, memoryhandler.Options{
		Source: string(model.MemorySourceAPI), KeyRequired: cfg.Memory.RequireKey, Keys: keySvc,
	})
	memoryConsoleHandler := memoryhandler.NewHandler(memorySvc, memoryhandler.Options{
		Source: string(model.MemorySourceConsole),
	})
	memoryMCPHandler := mcp.NewHandler(memorySvc, keySvc, cfg.Memory.RequireKey)

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
		Pricing:        admin.NewPricingHandler(costSvc),
		Holidays:       admin.NewHolidayHandler(holidaySvc),
		DataSettings:   admin.NewDataSettingsHandler(dataSvc),
		MimoSettings:   admin.NewMimoSettingsHandler(mimoSvc),
		ReasonRewrites: admin.NewReasoningRewriteHandler(reasoningSvc),
		Gateway:        gwhandler.NewHandler(exec, keySvc, cfg.Gateway.RequireKey, cfg.Gateway.MaxBodyBytes),
		MemoryEnabled:  cfg.Memory.Enabled,
		MemoryAgent:    memoryAgentHandler,
		MemoryConsole:  memoryConsoleHandler,
		MemoryAdmin:    admin.NewMemoryHandler(memorySvc, memoryClassifier),
		MemoryMCP:      memoryMCPHandler,
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
	aggCancel()
	priceCancel()
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
