package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		{"https://api.openai.com", ""},
		{"https://open.bigmodel.cn/api/paas/v4", ""},
		{"https://relay.example.com/deepseek", ""}, // 路径含厂家名不参与判断
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
	info, err := client.Fetch(context.Background(), provider.BalanceProviderDeepSeek, srv.URL+"/v1", "sk-test")
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
	_, err := client.Fetch(context.Background(), provider.BalanceProviderDeepSeek, srv.URL, "bad")
	if err == nil {
		t.Fatal("401 应返回错误")
	}

	// 非 JSON 响应 -> 解析错误
	srv2, _, _ := newBalanceUpstream(t, 200, `<html>oops</html>`)
	if _, err := client.Fetch(context.Background(), provider.BalanceProviderDeepSeek, srv2.URL, "k"); err == nil {
		t.Error("非 JSON 应返回错误")
	}

	// 非法 BaseURL
	if _, err := client.Fetch(context.Background(), provider.BalanceProviderDeepSeek, "://bad", "k"); err == nil {
		t.Error("非法 BaseURL 应返回错误")
	}

	// 不支持的厂家
	srv3, _, _ := newBalanceUpstream(t, 200, `{}`)
	if _, err := client.Fetch(context.Background(), "unknown", srv3.URL, "k"); err == nil {
		t.Error("未知厂家应返回错误")
	}
}
