package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xtokenhub/internal/model"
)

// forwardDeny 禁止透传到上游的入站请求头（键为小写）：鉴权头一律以渠道配置为准，
// 逐跳头与传输控制头由两端 HTTP 客户端自行协商，伪造来源头不外泄。
var forwardDeny = map[string]bool{
	"authorization":       true,
	"x-api-key":           true,
	"content-type":        true,
	"content-length":      true,
	"content-encoding":    true,
	"accept":              true,
	"accept-encoding":     true,
	"host":                true,
	"connection":          true,
	"keep-alive":          true,
	"proxy-authorization": true,
	"proxy-authenticate":  true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"expect":              true,
	"cookie":              true,
	"x-forwarded-for":     true,
	"x-forwarded-proto":   true,
	"x-forwarded-host":    true,
	"x-real-ip":           true,
}

// ForwardHeaders 将入站请求头透传到上游请求（跳过保留头），供会话路由类头
// （如 x-opencode-session）到达上游。src 为 nil 时为空操作。
func ForwardHeaders(dst, src http.Header) {
	for k, vs := range src {
		if forwardDeny[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// setUpstreamHeaders 装配发往上游的请求头：基础头、鉴权（opencode 渠道未配置
// key 时兜底免费 key）与厂家特化头（opencode 渠道注入 CLI 专属头模拟）。
// accept 按非流式/流式区分；须在 ForwardHeaders 之后调用，入站已携带的
// opencode 专属头由此得以保留。
func setUpstreamHeaders(h http.Header, baseURL string, providerType model.ProviderType, apiKey, accept string) {
	h.Set("Content-Type", "application/json")
	h.Set("Accept", accept)
	for k, v := range APIHeaders(providerType, openCodeUpstreamKey(baseURL, apiKey)) {
		h.Set(k, v)
	}
	applyOpenCodeHeaders(baseURL, h)
}

// UpstreamClient 上游 HTTP 客户端：透传与转换共用。
type UpstreamClient struct {
	http *http.Client
}

// NewUpstreamClient 构造客户端（timeout 为整体超时，流式调用应使用 DoStream 绕过）。
func NewUpstreamClient(timeout time.Duration) *UpstreamClient {
	return &UpstreamClient{http: &http.Client{Timeout: timeout}}
}

// Do 发送非流式请求。clientHeader 为调用方入站请求头（可 nil），透传会话路由类头。
func (c *UpstreamClient) Do(ctx context.Context, baseURL string, providerType model.ProviderType, apiKey string, p model.Protocol, body []byte, clientHeader http.Header) (*http.Response, error) {
	url, err := EndpointURL(baseURL, p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	ForwardHeaders(req.Header, clientHeader)
	setUpstreamHeaders(req.Header, baseURL, providerType, apiKey, "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("上游请求失败: %w", err)
	}
	return resp, nil
}

// DoStream 发送流式请求：不设整体超时（由 ctx/响应体生命周期控制），保留连接。
// clientHeader 为调用方入站请求头（可 nil），透传会话路由类头。
func (c *UpstreamClient) DoStream(ctx context.Context, baseURL string, providerType model.ProviderType, apiKey string, p model.Protocol, body []byte, clientHeader http.Header) (*http.Response, error) {
	url, err := EndpointURL(baseURL, p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	ForwardHeaders(req.Header, clientHeader)
	setUpstreamHeaders(req.Header, baseURL, providerType, apiKey, "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	// 流式连接不使用带超时的 client（避免长回答被整体超时截断）
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("上游流式请求失败: %w", err)
	}
	return resp, nil
}

// Drain 关闭响应体前丢弃剩余内容，帮助连接复用。
func Drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
