// Package gateway 网关端点 handler：解析入站协议请求并交给执行器。
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
)

// gin context 中传递调用方身份的键。
const (
	ctxKeyID   = "xt_key_id"
	ctxKeyName = "xt_key_name"
)

// KeyChecker 网关密钥校验接口（由 service.KeyService 实现）。
type KeyChecker interface {
	Check(ctx context.Context, raw string) (*model.APIKey, error)
}

// Handler 网关三端点。
type Handler struct {
	exec         *gateway.Executor
	maxBodyBytes int64
	keys         KeyChecker // 为 nil 时等同于不启用鉴权
	keyRequired  bool       // 是否强制校验密钥（gateway.require_key）
}

// NewHandler 构造。
func NewHandler(exec *gateway.Executor, keys KeyChecker, keyRequired bool, maxBodyBytes int64) *Handler {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 20 << 20
	}
	return &Handler{exec: exec, maxBodyBytes: maxBodyBytes, keys: keys, keyRequired: keyRequired}
}

// Register 注册网关端点（统一挂在密钥鉴权组下）。
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("", h.auth())
	g.GET("/v1/models", h.models)
	g.POST("/v1/chat/completions", h.chatCompletions)
	g.POST("/v1/responses", h.responses)
	g.POST("/v1/messages", h.messages)
}

// auth 网关密钥校验：Authorization: Bearer <key> 优先，其次 x-api-key。
// keyRequired=false 或未注入 KeyChecker 时放行（匿名调用按无 key 记日志）。
func (h *Handler) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.keyRequired || h.keys == nil {
			c.Next()
			return
		}
		raw := ExtractAPIKey(c.Request.Header)
		if raw == "" {
			h.unauthorized(c, "缺少 API Key")
			return
		}
		k, err := h.keys.Check(c.Request.Context(), raw)
		if err != nil {
			h.unauthorized(c, "无效的 API Key")
			return
		}
		c.Set(ctxKeyID, k.ID)
		c.Set(ctxKeyName, k.Name)
		c.Next()
	}
}

// ExtractAPIKey 从请求头提取网关密钥原文。
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

// unauthorized 按入站协议的错误格式返回 401。
func (h *Handler) unauthorized(c *gin.Context, msg string) {
	p := model.ProtocolChatCompletions
	if strings.HasPrefix(c.Request.URL.Path, "/v1/messages") {
		p = model.ProtocolMessages
	}
	writeClientError(c, p, http.StatusUnauthorized, msg)
	c.Abort()
}

// chatCompletions POST /v1/chat/completions
func (h *Handler) chatCompletions(c *gin.Context) {
	h.serve(c, model.ProtocolChatCompletions)
}

// responses POST /v1/responses
func (h *Handler) responses(c *gin.Context) {
	h.serve(c, model.ProtocolResponses)
}

// messages POST /v1/messages
func (h *Handler) messages(c *gin.Context) {
	h.serve(c, model.ProtocolMessages)
}

// models GET /v1/models：聚合各启用渠道配置的模型清单，供开发工具识别网关能力。
// 响应按 OpenAI 规范（object=list, data[].id/object/created/owned_by），
// 同时附带 Anthropic 的 type/display_name 与分页字段，两类客户端 SDK 均可直接解析。
func (h *Handler) models(c *gin.Context) {
	ms, err := h.exec.AvailableModels(c.Request.Context())
	if err != nil {
		writeClientError(c, model.ProtocolChatCompletions, http.StatusInternalServerError, "内部错误")
		return
	}
	now := time.Now().Unix()
	data := make([]gin.H, 0, len(ms))
	for _, id := range ms {
		data = append(data, gin.H{
			"id":           id,
			"object":       "model",
			"created":      now,
			"owned_by":     "xtokenhub",
			"type":         "model", // Anthropic /v1/models 字段
			"display_name": id,
		})
	}
	var first, last any
	if len(ms) > 0 {
		first, last = ms[0], ms[len(ms)-1]
	}
	c.JSON(http.StatusOK, gin.H{
		"object":   "list",
		"data":     data,
		"first_id": first, // Anthropic 分页字段
		"last_id":  last,
		"has_more": false,
	})
}

// serve 统一处理：读体 -> 校验/解析 -> 执行器。
func (h *Handler) serve(c *gin.Context, p model.Protocol) {
	body, err := readBody(c, h.maxBodyBytes)
	if err != nil {
		writeClientError(c, p, http.StatusBadRequest, err.Error())
		return
	}

	if err := provider.ValidateRequest(p, body); err != nil {
		writeClientError(c, p, http.StatusBadRequest, err.Error())
		return
	}

	conv, params, err := provider.ParseRequest(p, body)
	if err != nil {
		writeClientError(c, p, http.StatusBadRequest, err.Error())
		return
	}

	req := &gateway.Request{
		Protocol:     p,
		Conv:         &conv,
		Params:       params,
		Body:         body,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
		SessionID:    c.GetHeader("X-Session-Id"),
		ClientHeader: c.Request.Header,
	}
	if v, ok := c.Get(ctxKeyID); ok {
		req.KeyID, _ = v.(int64)
	}
	if v, ok := c.Get(ctxKeyName); ok {
		req.KeyName, _ = v.(string)
	}
	h.exec.Handle(c.Request.Context(), c.Writer, req)
}

// readBody 读取请求体（带上限保护）。
func readBody(c *gin.Context, limit int64) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit+1)
	defer c.Request.Body.Close()
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errors.New("请求体超过上限")
		}
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("请求体超过上限")
	}
	return data, nil
}

// writeClientError 按客户端协议格式返回 4xx 错误。
func writeClientError(c *gin.Context, p model.Protocol, status int, msg string) {
	c.Header("Content-Type", "application/json")
	c.Status(status)
	var body any
	switch p {
	case model.ProtocolMessages:
		body = map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": msg}}
	default:
		body = map[string]any{"error": map[string]any{"message": msg, "type": "invalid_request_error"}}
	}
	_ = json.NewEncoder(c.Writer).Encode(body)
}
