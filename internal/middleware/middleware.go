// Package middleware Gin 中间件：CORS、访问日志、Recovery。
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件（开发期 Vite 代理/同源部署，宽松放行）。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Api-Key, anthropic-version")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AccessLog 访问日志中间件。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("access",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"ip", c.ClientIP(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

// Recovery panic 恢复，返回统一错误响应。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic recovered",
					"panic", r,
					"path", c.Request.URL.Path,
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":    1003,
					"message": "内部错误",
				})
			}
		}()
		c.Next()
	}
}
