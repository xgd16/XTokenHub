// Package router 统一路由注册：管理 API、网关端点、静态资源 SPA 托管。
package router

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/config"
	"xtokenhub/internal/handler/admin"
	gwhandler "xtokenhub/internal/handler/gateway"
	mcp "xtokenhub/internal/handler/mcp"
	memory "xtokenhub/internal/handler/memory"
	"xtokenhub/internal/middleware"
)

// Deps 路由依赖集合。
type Deps struct {
	Cfg            *config.Config
	Channels       *admin.ChannelHandler
	CustomModels   *admin.CustomModelHandler
	Models         *admin.ModelHandler
	Keys           *admin.KeyHandler
	Logs           *admin.LogHandler
	Stats          *admin.StatsHandler
	Cleanup        *admin.CleanupHandler
	WS             *admin.WSHandler
	SessionHeaders *admin.SessionHeaderConfigHandler
	Pricing        *admin.PricingHandler
	Holidays       *admin.HolidayHandler
	DataSettings   *admin.DataSettingsHandler
	MimoSettings   *admin.MimoSettingsHandler
	ReasonRewrites *admin.ReasoningRewriteHandler
	Gateway        *gwhandler.Handler
	// MemoryEnabled 记忆库开关（关闭则三组路由都不注册）。
	MemoryEnabled bool
	// MemoryAgent 记忆库 Agent 独立 API（/memory/v1，key 鉴权）。
	MemoryAgent *memory.Handler
	// MemoryConsole 记忆库控制台 API（/api/v1/memories*，沿用管理端无鉴权约定）。
	MemoryConsole *memory.Handler
	// MemoryAdmin 记忆库管理（命名空间管理、LLM 分类）。
	MemoryAdmin *admin.MemoryHandler
	// MemoryMCP MCP 端点（/mcp）。
	MemoryMCP *mcp.Handler
	WebFS     fs.FS // 前端产物（可为 nil：仅 API）
}

// Register 注册全部路由。
func Register(engine *gin.Engine, d *Deps) {
	engine.Use(middleware.Recovery(), middleware.CORS(), middleware.AccessLog())

	// 健康检查
	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 管理端 API
	api := engine.Group("/api/v1")
	{
		api.GET("/ws", d.WS.Serve)
		api.GET("/channels", d.Channels.List)
		api.POST("/channels", d.Channels.Create)
		api.POST("/channels/lookup-models", d.Channels.LookupModels)
		api.GET("/channels/balances", d.Channels.Balances)
		api.GET("/channels/:id", d.Channels.Get)
		api.PUT("/channels/:id", d.Channels.Update)
		api.DELETE("/channels/:id", d.Channels.Delete)
		api.POST("/channels/:id/probe", d.Channels.Probe)
		api.GET("/custom-models", d.CustomModels.List)
		api.POST("/custom-models", d.CustomModels.Create)
		api.GET("/custom-models/:id", d.CustomModels.Get)
		api.PUT("/custom-models/:id", d.CustomModels.Update)
		api.DELETE("/custom-models/:id", d.CustomModels.Delete)
		api.GET("/models/usage", d.Models.Usage)
		api.GET("/keys", d.Keys.List)
		api.POST("/keys", d.Keys.Create)
		api.PUT("/keys/:id", d.Keys.Update)
		api.DELETE("/keys/:id", d.Keys.Delete)
		api.GET("/logs", d.Logs.List)
		api.GET("/logs/cleanup", d.Cleanup.Status)
		api.POST("/logs/cleanup", d.Cleanup.Trigger)
		api.GET("/stats/summary", d.Stats.Summary)
		api.GET("/stats/trend", d.Stats.Trend)
		api.GET("/stats/by-model", d.Stats.ByModel)
		api.GET("/stats/by-channel", d.Stats.ByChannel)
		api.GET("/stats/by-key", d.Stats.ByKey)
		api.GET("/stats/lifetime", d.Stats.Lifetime)
		api.GET("/stats/trend-by-model", d.Stats.TrendByModel)
		api.GET("/stats/live-sessions", d.Stats.LiveSessions)
		api.GET("/stats/cost/forecast", d.Pricing.Forecast)
		api.GET("/stats/cost/unpriced", d.Pricing.Unpriced)
		api.POST("/stats/cost/recompute", d.Pricing.Recompute)
		api.GET("/settings/session-headers", d.SessionHeaders.List)
		api.POST("/settings/session-headers", d.SessionHeaders.Create)
		api.DELETE("/settings/session-headers/:id", d.SessionHeaders.Delete)
		api.PUT("/settings/session-headers/:key/toggle", d.SessionHeaders.ToggleEnabled)
		api.GET("/settings/prices", d.Pricing.ListPrices)
		api.POST("/settings/prices", d.Pricing.CreatePrice)
		api.POST("/settings/prices/sync", d.Pricing.SyncPrices)
		api.PUT("/settings/prices/:id", d.Pricing.UpdatePrice)
		api.DELETE("/settings/prices/:id", d.Pricing.DeletePrice)
		api.GET("/settings/billing", d.Pricing.GetBilling)
		api.PUT("/settings/billing", d.Pricing.UpdateBilling)
		api.GET("/settings/holidays", d.Holidays.List)
		api.POST("/settings/holidays/sync", d.Holidays.Sync)
		api.GET("/settings/data", d.DataSettings.Get)
		api.PUT("/settings/data", d.DataSettings.Update)
		api.POST("/settings/data/aggregate/rebuild", d.DataSettings.RebuildAggregate)
		api.GET("/settings/mimo", d.MimoSettings.Get)
		api.PUT("/settings/mimo", d.MimoSettings.Update)
		api.GET("/reasoning-rewrites", d.ReasonRewrites.List)
		api.POST("/reasoning-rewrites", d.ReasonRewrites.Create)
		api.GET("/reasoning-rewrites/:id", d.ReasonRewrites.Get)
		api.PUT("/reasoning-rewrites/:id", d.ReasonRewrites.Update)
		api.DELETE("/reasoning-rewrites/:id", d.ReasonRewrites.Delete)

		// 记忆库管理（控制台专用：命名空间管理、LLM 一键分类、批量操作）
		if d.MemoryAdmin != nil {
			api.GET("/memory-namespaces", d.MemoryAdmin.ListNamespaces)
			api.POST("/memory-namespaces", d.MemoryAdmin.CreateNamespace)
			api.POST("/memory-namespaces/rename", d.MemoryAdmin.RenameNamespace)
			api.POST("/memory-namespaces/merge", d.MemoryAdmin.MergeNamespaces)
			api.PATCH("/memory-namespaces/:id", d.MemoryAdmin.UpdateNamespace)
			api.DELETE("/memory-namespaces/:id", d.MemoryAdmin.DeleteNamespace)
			api.POST("/memories/batch-move", d.MemoryAdmin.BatchMove)
			api.POST("/memories/auto-link", d.MemoryAdmin.RebuildAutoLinks)
			api.GET("/memories/classify-config", d.MemoryAdmin.ClassifyConfig)
			api.PUT("/memories/classify-config", d.MemoryAdmin.UpdateClassifyConfig)
			api.POST("/memories/classify", d.MemoryAdmin.TriggerClassify)
			api.GET("/memories/classify", d.MemoryAdmin.ClassifyStatus)
		}
		// 记忆库控制台 API（与 /memory/v1 同一实现，写入身份为 console）
		if d.MemoryConsole != nil {
			d.MemoryConsole.RegisterConsole(api)
		}
	}

	// 记忆库 Agent 独立 API（鉴权头同网关：Authorization: Bearer / x-api-key）
	if d.MemoryEnabled && d.MemoryAgent != nil {
		mem := engine.Group("/memory/v1", d.MemoryAgent.Auth())
		d.MemoryAgent.Register(mem)
	}

	// MCP 端点（Streamable HTTP）
	if d.MemoryEnabled && d.MemoryMCP != nil {
		d.MemoryMCP.Register(engine)
	}

	// 网关端点
	d.Gateway.Register(engine)

	// 前端静态资源 + SPA fallback（/api、/v1 不受影响）
	if d.WebFS != nil {
		registerStatic(engine, d.WebFS)
	}
}

// registerStatic 挂载嵌入的前端产物：静态文件直出，其余路径回退 index.html。
func registerStatic(engine *gin.Engine, webFS fs.FS) {
	fileServer := http.StripPrefix("/", http.FileServer(http.FS(webFS)))
	engine.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, "/v1") {
			// 尝试命中静态文件
			if path != "/" {
				f, err := webFS.Open(strings.TrimPrefix(path, "/"))
				if err == nil {
					_ = f.Close()
					fileServer.ServeHTTP(c.Writer, c.Request)
					return
				}
			}
			// SPA fallback
			index, err := fs.ReadFile(webFS, "index.html")
			if err == nil {
				c.Data(http.StatusOK, "text/html; charset=utf-8", index)
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"code": 1002, "message": "not found"})
	})
}
