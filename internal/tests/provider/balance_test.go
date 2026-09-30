package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xtokenhub/internal/provider"
)

func TestInferBalanceProvider(t *testing.T) {
	cases := []struct {
		baseURL string
		want    provider.BalanceProviderKind
	}{
		{"https://api.deepseek.com", provider.BalanceProviderDeepSeek},
		{"https://api.deepseek.com/v1", provider.BalanceProviderDeepSeek},
		{"https://api.deepseek.com/anthropic", provider.BalanceProviderDeepSeek},
		{"https://api.deepseek.com/anthropic/", provider.BalanceProviderDeepSeek},
		{"https://API.DeepSeek.com", provider.BalanceProviderDeepSeek},
		{"https://token-plan-cn.xiaomimimo.com", provider.BalanceProviderMiMo},
		{"https://token-plan-cn.xiaomimimo.com/anthropic", provider.BalanceProviderMiMo},
		{"https://Platform.XiaoMiMiMo.com", provider.BalanceProviderMiMo},
		{"https://api.openai.com", ""},
		{"https://open.bigmodel.cn/api/paas/v4", ""},
		{"https://relay.example.com/deepseek", ""},   // 路径含厂家名不参与判断
		{"https://relay.example.com/xiaomimimo", ""}, // 同上
		{"not a url", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := provider.InferBalanceProvider(c.baseURL); got != c.want {
			t.Errorf("InferBalanceProvider(%q) = %q, want %q", c.baseURL, got, c.want)
		}
	}
}

// newBalanceUpstream 假 DeepSeek 余额上游，返回命中计数与最近一次 Authorization 头。
func newBalanceUpstream(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int64, *atomic.Value) {
	t.Helper()
	var hits atomic.Int64
	var lastAuth atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/user/balance", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lastAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits, &lastAuth
}

func TestBalanceFetchSuccess(t *testing.T) {
	srv, hits, lastAuth := newBalanceUpstream(t, 200, `{
		"is_available": true,
		"balance_infos": [{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]
	}`)
	client := provider.NewBalanceClient(2 * time.Second)

	// base_url 带 /v1：余额端点应剥到主域名根路径
	info, err := client.Fetch(context.Background(), provider.BalanceProviderDeepSeek, srv.URL+"/v1", "sk-test", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("上游命中次数 = %d", hits.Load())
	}
	if got, _ := lastAuth.Load().(string); got != "Bearer sk-test" {
		t.Errorf("Authorization = %q", got)
	}
	if !info.Available || info.Currency != "CNY" {
		t.Errorf("available/currency = %v/%q", info.Available, info.Currency)
	}
	if info.Total != 110 || info.Granted != 10 || info.ToppedUp != 100 {
		t.Errorf("金额解析错误: %+v", info)
	}
	if info.FetchedAt.IsZero() {
		t.Error("FetchedAt 未回填")
	}
}

func TestBalanceFetchErrors(t *testing.T) {
	// 401 -> 明确错误信息
	srv, _, _ := newBalanceUpstream(t, 401, `{"error":{"message":"auth failed"}}`)
	client := provider.NewBalanceClient(2 * time.Second)
	_, err := client.Fetch(context.Background(), provider.BalanceProviderDeepSeek, srv.URL, "bad", "")
	if err == nil {
		t.Fatal("401 应返回错误")
	}

	// 非 JSON 响应 -> 解析错误
	srv2, _, _ := newBalanceUpstream(t, 200, `<html>oops</html>`)
	if _, err := client.Fetch(context.Background(), provider.BalanceProviderDeepSeek, srv2.URL, "k", ""); err == nil {
		t.Error("非 JSON 应返回错误")
	}

	// 非法 BaseURL
	if _, err := client.Fetch(context.Background(), provider.BalanceProviderDeepSeek, "://bad", "k", ""); err == nil {
		t.Error("非法 BaseURL 应返回错误")
	}

	// 不支持的厂家
	srv3, _, _ := newBalanceUpstream(t, 200, `{}`)
	if _, err := client.Fetch(context.Background(), "unknown", srv3.URL, "k", ""); err == nil {
		t.Error("未知厂家应返回错误")
	}
}

// newMimoUpstream 假 MiMo 平台余额上游：记录最近一次 Cookie / UA / Authorization 头。
func newMimoUpstream(t *testing.T, status int, body string) (*httptest.Server, *atomic.Value, *atomic.Value, *atomic.Value) {
	t.Helper()
	var lastCookie, lastUA, lastAuth atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/balance", func(w http.ResponseWriter, r *http.Request) {
		lastCookie.Store(r.Header.Get("Cookie"))
		lastUA.Store(r.Header.Get("User-Agent"))
		lastAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &lastCookie, &lastUA, &lastAuth
}

func TestBalanceFetchMiMoSuccess(t *testing.T) {
	srv, lastCookie, lastUA, lastAuth := newMimoUpstream(t, 200, `{
		"code":0,"message":"",
		"data":{"balance":"29.21","frozenBalance":"0.00","currency":"CNY",
			"overdraftLimit":"0.00","remainingOverdraftLimit":"0.00",
			"giftBalance":"1.21","cashBalance":"28.00"}
	}`)
	client := provider.NewBalanceClient(2 * time.Second)
	client.SetMimoEndpoint(srv.URL + "/api/v1/balance")

	const cookie = `userId=266075060; api-platform_serviceToken="tok="`
	// base_url 为转发主机，余额端点固定在平台域名，与它无关
	info, err := client.Fetch(context.Background(), provider.BalanceProviderMiMo, "https://token-plan-cn.xiaomimimo.com", "tp-key", cookie)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, _ := lastCookie.Load().(string); got != cookie {
		t.Errorf("Cookie 头 = %q", got)
	}
	if got, _ := lastAuth.Load().(string); got != "" {
		t.Errorf("不应发送 Authorization 头, got %q", got)
	}
	if got, _ := lastUA.Load().(string); got == "" {
		t.Error("应携带浏览器 UA")
	}
	if !info.Available || info.Currency != "CNY" {
		t.Errorf("available/currency = %v/%q", info.Available, info.Currency)
	}
	if info.Total != 29.21 || info.Granted != 1.21 || info.ToppedUp != 28.00 {
		t.Errorf("金额解析错误: %+v", info)
	}
	if info.FetchedAt.IsZero() {
		t.Error("FetchedAt 未回填")
	}
}

func TestBalanceFetchMiMoErrors(t *testing.T) {
	client := provider.NewBalanceClient(2 * time.Second)
	// 未配置 Cookie：不发出请求，直接报可操作的错误
	srv, lastCookie, _, _ := newMimoUpstream(t, 200, `{}`)
	client.SetMimoEndpoint(srv.URL + "/api/v1/balance")
	_, err := client.Fetch(context.Background(), provider.BalanceProviderMiMo, "https://token-plan-cn.xiaomimimo.com", "k", "  ")
	if err == nil || !strings.Contains(err.Error(), "未配置 MiMo 网页 Cookie") {
		t.Fatalf("空 Cookie 应报未配置, got %v", err)
	}
	if lastCookie.Load() != nil {
		t.Error("空 Cookie 不应发起请求")
	}

	// HTTP 401：凭证过期文案
	srv2, _, _, _ := newMimoUpstream(t, 401, `{"code":401,"loginUrl":"https://account.xiaomi.com/..."}`)
	client.SetMimoEndpoint(srv2.URL + "/api/v1/balance")
	_, err = client.Fetch(context.Background(), provider.BalanceProviderMiMo, "", "k", "a=1")
	if err == nil || !strings.Contains(err.Error(), "凭证已过期") {
		t.Fatalf("401 应报凭证过期, got %v", err)
	}

	// 200 包体携带 code:401：同样按过期处理
	srv3, _, _, _ := newMimoUpstream(t, 200, `{"code":401,"message":""}`)
	client.SetMimoEndpoint(srv3.URL + "/api/v1/balance")
	if _, err = client.Fetch(context.Background(), provider.BalanceProviderMiMo, "", "k", "a=1"); err == nil || !strings.Contains(err.Error(), "凭证已过期") {
		t.Fatalf("包体 code:401 应报凭证过期, got %v", err)
	}

	// 其余非 0 code：原样带出
	srv4, _, _, _ := newMimoUpstream(t, 200, `{"code":500,"message":"boom"}`)
	client.SetMimoEndpoint(srv4.URL + "/api/v1/balance")
	if _, err = client.Fetch(context.Background(), provider.BalanceProviderMiMo, "", "k", "a=1"); err == nil || !strings.Contains(err.Error(), "code=500") {
		t.Fatalf("非 0 code 应带出错误, got %v", err)
	}
}
