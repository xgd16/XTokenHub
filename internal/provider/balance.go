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

// BalanceProviderMiMo 小米 MiMo 开放平台余额：官方未开放 API，用网页控制台的
// 会话 Cookie 重放 GET /api/v1/balance（平台域名，与转发用的 token-plan 主机不同）。
const BalanceProviderMiMo BalanceProviderKind = "mimo"

// mimoBalanceEndpoint MiMo 网页控制台余额接口（固定平台域名，不从渠道 BaseURL 推导）。
const mimoBalanceEndpoint = "https://platform.xiaomimimo.com/api/v1/balance"

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
	if strings.Contains(host, "xiaomimimo") {
		return BalanceProviderMiMo
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
	http         *http.Client
	timeout      time.Duration
	mimoEndpoint string
}

// NewBalanceClient 构造余额客户端（timeout <= 0 取默认 15s）。
func NewBalanceClient(timeout time.Duration) *BalanceClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &BalanceClient{http: &http.Client{Timeout: timeout}, timeout: timeout, mimoEndpoint: mimoBalanceEndpoint}
}

// SetMimoEndpoint 覆盖 MiMo 余额端点（测试注入假上游用）。
func (c *BalanceClient) SetMimoEndpoint(endpoint string) {
	c.mimoEndpoint = endpoint
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

// Fetch 查询上游账户余额。DeepSeek 用渠道配置的厂家 API Key（Bearer 鉴权）；
// MiMo 用网页控制台会话 Cookie（端点固定在平台域名，与转发用的 token-plan 主机无关）。
func (c *BalanceClient) Fetch(ctx context.Context, kind BalanceProviderKind, baseURL, apiKey, cookie string) (*BalanceInfo, error) {
	endpoint := c.mimoEndpoint
	if kind != BalanceProviderMiMo {
		var err error
		endpoint, err = balanceEndpoint(kind, baseURL)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	switch kind {
	case BalanceProviderMiMo:
		cookie = strings.TrimSpace(cookie)
		if cookie == "" {
			return nil, fmt.Errorf("未配置 MiMo 网页 Cookie，请在 设置 → MiMo 余额凭证 中填写")
		}
		// 网页接口按浏览器对待：补 UA，整串 Cookie 原样转发（含引号形式的值）
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
		req.Header.Set("Cookie", cookie)
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer Drain(resp)
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	// 凭证过期时上游返回 HTTP 401（body 亦为 code:401 + loginUrl），给可操作的提示
	if kind == BalanceProviderMiMo && resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("MiMo 网页登录凭证已过期，请在 设置 → MiMo 余额凭证 更新 Cookie")
	}
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

// mimoBalanceResp MiMo GET /api/v1/balance 响应：信封 code=0 成功；金额为字符串。
type mimoBalanceResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Balance       string `json:"balance"`
		FrozenBalance string `json:"frozenBalance"`
		Currency      string `json:"currency"`
		GiftBalance   string `json:"giftBalance"`
		CashBalance   string `json:"cashBalance"`
	} `json:"data"`
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
	case BalanceProviderMiMo:
		var r mimoBalanceResp
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("解析余额响应: %w", err)
		}
		// 防御：凭证过期通常表现为 HTTP 401，这里兜住 200 包体携带 code:401 的形态
		if r.Code == 401 {
			return nil, fmt.Errorf("MiMo 网页登录凭证已过期，请在 设置 → MiMo 余额凭证 更新 Cookie")
		}
		if r.Code != 0 {
			return nil, fmt.Errorf("MiMo 余额接口返回 code=%d: %s", r.Code, r.Message)
		}
		return &BalanceInfo{
			Provider:  kind,
			Available: true, // 网页控制台无「账户可用性」概念，能查出余额即可用
			Currency:  r.Data.Currency,
			Total:     parseAmount(r.Data.Balance),
			Granted:   parseAmount(r.Data.GiftBalance),
			ToppedUp:  parseAmount(r.Data.CashBalance),
			FetchedAt: time.Now(),
		}, nil
	default:
		return nil, fmt.Errorf("不支持的余额查询厂家: %s", kind)
	}
}

// parseAmount 上游金额为字符串（如 "110.00"），非法值按 0 处理。
func parseAmount(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}
