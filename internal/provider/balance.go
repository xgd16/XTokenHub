package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BalanceProviderKind 支持余额查询的上游厂家标识（按渠道 BaseURL 推断）。
type BalanceProviderKind string

// BalanceProviderDeepSeek DeepSeek 账户余额（官方接口 GET /user/balance）。
const BalanceProviderDeepSeek BalanceProviderKind = "deepseek"

// InferBalanceProvider 按 BaseURL 判断渠道可查询哪家的账户余额；空串 = 不支持。
// 仅按 host 匹配（如 api.deepseek.com），路径不参与判断——避免把路径含厂家名的
// 中转站误判为官方端点。
func InferBalanceProvider(baseURL string) BalanceProviderKind {
	root, ok := NormalizeBaseURL(baseURL)
	if !ok {
		return ""
	}
	u, err := url.Parse(root)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "deepseek") {
		return BalanceProviderDeepSeek
	}
	return ""
}

// BalanceInfo 上游账户余额快照（金额由上游字符串解析，保留两位小数展示）。
type BalanceInfo struct {
	Provider  BalanceProviderKind `json:"provider"`
	Available bool                `json:"is_available"`
	Currency  string              `json:"currency"`
	Total     float64             `json:"total"`
	Granted   float64             `json:"granted"`   // 赠金
	ToppedUp  float64             `json:"topped_up"` // 充值
	FetchedAt time.Time           `json:"fetched_at"`
}

// BalanceClient 上游账户余额查询客户端。
type BalanceClient struct {
	http    *http.Client
	timeout time.Duration
}

// NewBalanceClient 构造余额客户端（timeout <= 0 取默认 15s）。
func NewBalanceClient(timeout time.Duration) *BalanceClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &BalanceClient{http: &http.Client{Timeout: timeout}, timeout: timeout}
}

// balanceEndpoint 各厂家余额查询端点。DeepSeek 的账户接口挂主域名根路径
// （BaseURL 的 /v1、/anthropic 等挂载段不参与），故仅取 scheme://host。
func balanceEndpoint(kind BalanceProviderKind, baseURL string) (string, error) {
	root, ok := NormalizeBaseURL(baseURL)
	if !ok {
		return "", errInvalidBaseURL(baseURL)
	}
	u, err := url.Parse(root)
	if err != nil {
		return "", errInvalidBaseURL(baseURL)
	}
	switch kind {
	case BalanceProviderDeepSeek:
		return u.Scheme + "://" + u.Host + "/user/balance", nil
	default:
		return "", fmt.Errorf("不支持的余额查询厂家: %s", kind)
	}
}

// Fetch 查询上游账户余额。apiKey 为渠道配置的厂家 API Key。
func (c *BalanceClient) Fetch(ctx context.Context, kind BalanceProviderKind, baseURL, apiKey string) (*BalanceInfo, error) {
	endpoint, err := balanceEndpoint(kind, baseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer Drain(resp)
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s → HTTP %d: %s", endpoint, resp.StatusCode, ExtractErrorDetail(data))
	}
	return parseBalance(kind, data)
}

// deepseekBalanceResp DeepSeek GET /user/balance 响应。
// balance_infos 按币种列出，官方账户单一币种，取第一条。
type deepseekBalanceResp struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}

func parseBalance(kind BalanceProviderKind, data []byte) (*BalanceInfo, error) {
	switch kind {
	case BalanceProviderDeepSeek:
		var r deepseekBalanceResp
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("解析余额响应: %w", err)
		}
		info := &BalanceInfo{Provider: kind, Available: r.IsAvailable, FetchedAt: time.Now()}
		if len(r.BalanceInfos) > 0 {
			b := r.BalanceInfos[0]
			info.Currency = b.Currency
			info.Total = parseAmount(b.TotalBalance)
			info.Granted = parseAmount(b.GrantedBalance)
			info.ToppedUp = parseAmount(b.ToppedUpBalance)
		}
		return info, nil
	default:
		return nil, fmt.Errorf("不支持的余额查询厂家: %s", kind)
	}
}

// parseAmount 上游金额为字符串（如 "110.00"），非法值按 0 处理。
func parseAmount(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}
