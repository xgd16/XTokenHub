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
	Gateway        *gwhandler.Handler
	WebFS          fs.FS // 前端产物（可为 nil：仅 API）
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
		api.GET("/settings/session-headers", d.SessionHeaders.List)
		api.POST("/settings/session-headers", d.SessionHeaders.Create)
		api.DELETE("/settings/session-headers/:id", d.SessionHeaders.Delete)
		api.PUT("/settings/session-headers/:key/toggle", d.SessionHeaders.ToggleEnabled)
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
