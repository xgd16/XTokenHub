package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/resp"
)

// gin 上下文中调用方身份的键（与网关保持一致，便于跨端点读取）。
const (
	CtxKeyID   = "xt_key_id"
	CtxKeyName = "xt_key_name"
)

// KeyChecker API Key 校验接口（由 service.KeyService 实现）。
type KeyChecker interface {
	Check(ctx context.Context, raw string) (*model.APIKey, error)
}

// APIKeyAuth API Key 鉴权中间件：Authorization: Bearer 优先，其次 x-api-key。
// keyRequired=false 或未注入校验器时放行（匿名调用按 anonymous 记来源）；
// 通过后把 key 的 ID/Name 写入上下文供 handler 取用。
func APIKeyAuth(keys KeyChecker, keyRequired bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !keyRequired || keys == nil {
			c.Next()
			return
		}
		raw := ExtractAPIKey(c.Request.Header)
		if raw == "" {
			unauthorized(c, "缺少 API Key")
			return
		}
		k, err := keys.Check(c.Request.Context(), raw)
		if err != nil {
			unauthorized(c, "无效的 API Key")
			return
		}
		c.Set(CtxKeyID, k.ID)
		c.Set(CtxKeyName, k.Name)
		c.Next()
	}
}

// ExtractAPIKey 从请求头提取 API Key 原文。
func ExtractAPIKey(header http.Header) string {
	if auth := header.Get("Authorization"); auth != "" {
		const prefix = "Bearer "
		if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
		return strings.TrimSpace(auth) // 无 Bearer 前缀时按原文兼容处理
	}
	if v := header.Get("X-Api-Key"); v != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

// unauthorized 401 响应（统一信封，HTTP 状态码区分鉴权失败）。
func unauthorized(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, resp.Envelope{
		Code: 1001, Message: msg,
	})
}
