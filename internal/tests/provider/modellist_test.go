package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
)

func TestModelsURL(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		want    string
		wantErr bool
	}{
		{"域名根", "https://api.openai.com", "https://api.openai.com/v1/models", false},
		{"带v1与尾斜杠", "https://api.anthropic.com/v1/", "https://api.anthropic.com/v1/models", false},
		{"版本化挂载(智谱)", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/models", false},
		{"http", "http://localhost:9000", "http://localhost:9000/v1/models", false},
		{"空URL", "  ", "", true},
		{"非http前缀", "ftp://x.com", "", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := provider.ModelsURL(tt.base)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchModelsOpenAI(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m-b"},{"id":"m-a"},{"id":"m-b"},{"id":""}]}`))
	}))
	defer srv.Close()

	pr := provider.NewProber(2 * time.Second)
	ms, err := pr.FetchModels(context.Background(), srv.URL, model.ProviderOpenAICompatible, "sk-k")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/models" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer sk-k" {
		t.Errorf("auth header = %q", gotAuth)
	}
	// 去重、剔除空 id、保持上游顺序
	if len(ms) != 2 || ms[0] != "m-b" || ms[1] != "m-a" {
		t.Errorf("models = %v", ms)
	}
}

func TestFetchModelsAnthropic(t *testing.T) {
	var gotAPIKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-5"},{"id":"claude-haiku-4-5"}]}`))
	}))
	defer srv.Close()

	pr := provider.NewProber(2 * time.Second)
	// 带 /v1 结尾的写法不应拼出 /v1/v1/models
	ms, err := pr.FetchModels(context.Background(), srv.URL+"/v1", model.ProviderAnthropic, "ak")
	if err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "ak" || gotVersion != "2023-06-01" {
		t.Errorf("headers = %q %q", gotAPIKey, gotVersion)
	}
	if len(ms) != 2 {
		t.Errorf("models = %v", ms)
	}
}

// 兼容部分中转站直接返回数组的写法；空列表不视为错误。
func TestFetchModelsTolerantBodies(t *testing.T) {
	pr := provider.NewProber(2 * time.Second)
	ctx := context.Background()
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"裸数组", `[{"id":"m1"},{"id":"m2"}]`, []string{"m1", "m2"}},
		{"空列表", `{"data":[]}`, []string{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			ms, err := pr.FetchModels(ctx, srv.URL, model.ProviderOpenAICompatible, "k")
			if err != nil {
				t.Fatal(err)
			}
			if len(ms) != len(tt.want) {
				t.Errorf("models = %v, want %v", ms, tt.want)
			}
		})
	}
}

func TestFetchModelsErrors(t *testing.T) {
	pr := provider.NewProber(2 * time.Second)
	ctx := context.Background()

	// 非法 BaseURL
	if _, err := pr.FetchModels(ctx, "ftp://x", model.ProviderOpenAICompatible, "k"); err == nil {
		t.Error("非法 BaseURL 应报错")
	}

	// 上游鉴权失败
	srv401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv401.Close()
	if _, err := pr.FetchModels(ctx, srv401.URL, model.ProviderOpenAICompatible, "k"); err == nil {
		t.Error("上游 401 应报错")
	} else if !strings.Contains(err.Error(), "401") {
		t.Errorf("错误应包含状态码: %v", err)
	}

	// 响应体不是 JSON（如网关返回 HTML）
	srvHTML := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>oops</html>"))
	}))
	defer srvHTML.Close()
	if _, err := pr.FetchModels(ctx, srvHTML.URL, model.ProviderOpenAICompatible, "k"); err == nil {
		t.Error("非 JSON 响应应报错")
	}
}

// 子路径 BaseURL 是协议挂载点：只探测该厂家协议家族，绝不发跨家族探测请求。
func TestProbeSubPathFamilyOnly(t *testing.T) {
	mux := http.NewServeMux()
	hits := map[string]int{}
	mux.HandleFunc("/anthropic/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		hits["POST /anthropic/v1/messages"]++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	})
	mux.HandleFunc("/anthropic/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		hits["GET /anthropic/v1/models"]++
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		hits["GET /v1/models"]++
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-chat"}]}`))
	})
	// 其它任何路径（如 /anthropic/v1/chat/completions）都不应被请求
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pr := provider.NewProber(2 * time.Second)
	report := pr.Probe(context.Background(), srv.URL+"/anthropic", "ak", model.ProviderAnthropic, "")

	if len(report.Items) != 1 || report.Items[0].Protocol != model.ProtocolMessages || !report.Items[0].OK {
		t.Fatalf("子路径只探测家族协议: %+v", report.Items)
	}
	if hits["POST /anthropic/v1/chat/completions"] != 0 || hits["POST /anthropic/v1/responses"] != 0 {
		t.Errorf("不应向子路径发跨家族探测: %v", hits)
	}

	// 裸域名仍全量探测三协议（中转站多协议自动识别）
	report2 := pr.Probe(context.Background(), srv.URL, "ak", model.ProviderAnthropic, "")
	if len(report2.Items) != 3 {
		t.Fatalf("裸域名应全量探测: %+v", report2.Items)
	}
}

// DeepSeek 场景：子路径 BaseURL 自身 /v1/models 不可用，降级主域名 /v1/models。
func TestFetchModelsFallbackToMainDomain(t *testing.T) {
	mux := http.NewServeMux()
	subHits, rootHits := 0, 0
	var rootAuth http.Header
	mux.HandleFunc("/anthropic/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		subHits++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid URL"}}`))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		rootHits++
		rootAuth = r.Header
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pr := provider.NewProber(2 * time.Second)
	ctx := context.Background()

	// Fallback：子路径 404 -> 主域名成功
	ms, err := pr.FetchModelsFallback(ctx, srv.URL+"/anthropic", model.ProviderAnthropic, "ak")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0] != "deepseek-chat" {
		t.Errorf("应取主域名列表, got %v", ms)
	}
	if subHits != 1 || rootHits != 1 {
		t.Errorf("hits sub=%d root=%d", subHits, rootHits)
	}
	// 主域名兜底同时携带两种鉴权头（anthropic 渠道降级到厂家 OpenAI 面）
	if rootAuth.Get("Authorization") != "Bearer ak" || rootAuth.Get("x-api-key") != "ak" {
		t.Errorf("主域名兜底应带双鉴权头: %v", rootAuth)
	}

	// FetchModels（仅渠道自身端点）不受主域名兜底影响
	if _, err := pr.FetchModels(ctx, srv.URL+"/anthropic", model.ProviderAnthropic, "ak"); err == nil {
		t.Error("仅自身端点失败时应报错")
	} else if !strings.Contains(err.Error(), "/anthropic/v1/models") {
		t.Errorf("错误应带完整端点 URL: %v", err)
	}

	// 无子路径的 BaseURL 只有一个候选端点
	solo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer solo.Close()
	if _, err := pr.FetchModelsFallback(ctx, solo.URL, model.ProviderOpenAICompatible, "k"); err == nil {
		t.Error("两级均失败应报错")
	}
}

// 探测失败的详情应包含完整请求端点，便于定位实际请求路径。
func TestProbeDetailIncludesURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid URL"}}`))
	}))
	defer srv.Close()

	pr := provider.NewProber(2 * time.Second)
	report := pr.Probe(context.Background(), srv.URL+"/anthropic", "ak", model.ProviderAnthropic, "")
	if len(report.Items) != 1 {
		t.Fatalf("items = %+v", report.Items)
	}
	detail := report.Items[0].Detail
	if !strings.Contains(detail, "POST "+srv.URL+"/anthropic/v1/messages") {
		t.Errorf("详情应包含完整端点: %q", detail)
	}
	if !strings.Contains(detail, "HTTP 404") {
		t.Errorf("详情应包含状态码: %q", detail)
	}
}

// 子路径自身列表可用时优先采用，不请求主域名；主域名兜底保留渠道自身端点的首个错误。
func TestFetchModelsFallbackPrefersSubPath(t *testing.T) {
	mux := http.NewServeMux()
	rootHits := 0
	mux.HandleFunc("/openai/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"sub-model"}]}`))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		rootHits++
		_, _ = w.Write([]byte(`{"data":[{"id":"root-model"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pr := provider.NewProber(2 * time.Second)
	ms, err := pr.FetchModelsFallback(context.Background(), srv.URL+"/openai", model.ProviderOpenAICompatible, "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0] != "sub-model" {
		t.Errorf("应优先渠道自身列表, got %v", ms)
	}
	if rootHits != 0 {
		t.Error("自身列表可用时不应请求主域名")
	}
}

// 智谱 GLM 场景：版本化挂载点 /api/paas/v4，models 与协议端点均直接拼资源路径，
// 不插入 /v1；自身列表可用时不降级主域名。
func TestVersionedMountGLM(t *testing.T) {
	mux := http.NewServeMux()
	rootHits := 0
	mux.HandleFunc("/api/paas/v4/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"glm-4.7"},{"id":"glm-4.7-flash"}]}`))
	})
	mux.HandleFunc("/api/paas/v4/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","choices":[{"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	})
	mux.HandleFunc("/v1/models", func(_ http.ResponseWriter, _ *http.Request) {
		rootHits++
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	base := srv.URL + "/api/paas/v4"
	pr := provider.NewProber(2 * time.Second)
	ctx := context.Background()

	ms, err := pr.FetchModels(ctx, base, model.ProviderOpenAICompatible, "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0] != "glm-4.7" {
		t.Errorf("models = %v", ms)
	}

	report := pr.Probe(ctx, base, "k", model.ProviderOpenAICompatible, "glm-4.7")
	if len(report.Items) != 2 {
		t.Fatalf("子路径家族探测应为 2 项: %+v", report.Items)
	}
	if !report.Items[0].OK || report.Items[0].Protocol != model.ProtocolChatCompletions {
		t.Errorf("chat_completions 应原生可用: %+v", report.Items[0])
	}
	if report.Items[1].Protocol != model.ProtocolResponses && report.Items[1].OK {
		t.Errorf("responses 不应误判原生: %+v", report.Items[1])
	}

	// Fallback 第一候选即 …/v4/models（可用），不应降级主域名
	if _, err := pr.FetchModelsFallback(ctx, base, model.ProviderOpenAICompatible, "k"); err != nil {
		t.Fatal(err)
	}
	if rootHits != 0 {
		t.Errorf("版本化挂载自身列表可用时不应请求主域名, hits=%d", rootHits)
	}
}
