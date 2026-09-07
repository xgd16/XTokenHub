package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupModelsEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"no models api"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"hm-1"},{"id":"hm-2"}]}`))
	}))
	t.Cleanup(srv.Close)
	_, api := newTestEnv(t)

	// 参数缺失 → 400 业务参数错误
	code, env, _ := doJSON(t, api, "POST", "/api/v1/channels/lookup-models", map[string]any{})
	if code != 400 || env["code"] != float64(1001) {
		t.Fatalf("缺参: %d %v", code, env)
	}

	// 非法接口风格
	code, env, _ = doJSON(t, api, "POST", "/api/v1/channels/lookup-models", map[string]any{
		"provider": "nope", "base_url": srv.URL, "api_key": "k",
	})
	if code != 400 || env["code"] != float64(1001) {
		t.Fatalf("非法 provider: %d %v", code, env)
	}

	// 正常拉取
	code, env, _ = doJSON(t, api, "POST", "/api/v1/channels/lookup-models", map[string]any{
		"provider": "openai_compatible", "base_url": srv.URL, "api_key": "k",
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("lookup: %d %v", code, env)
	}
	data := env["data"].(map[string]any)
	models := data["models"].([]any)
	if len(models) != 2 || models[0] != "hm-1" {
		t.Fatalf("models = %v", models)
	}

	// 上游无模型列表接口 → 业务失败响应（带上游错误详情）
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"no models api"}}`))
	}))
	t.Cleanup(bare.Close)
	code, env, _ = doJSON(t, api, "POST", "/api/v1/channels/lookup-models", map[string]any{
		"provider": "openai_compatible", "base_url": bare.URL, "api_key": "k",
	})
	if code == 200 && env["code"] == float64(0) {
		t.Fatal("上游无列表接口时应返回业务失败")
	}
	if env["message"] == nil || env["message"].(string) == "" {
		t.Fatalf("应带错误详情: %v", env)
	}

	// 省略 provider → 按 base_url 自动推断（挂载点含 anthropic → 子路径 404 → 降级主域名）
	code, env, _ = doJSON(t, api, "POST", "/api/v1/channels/lookup-models", map[string]any{
		"base_url": srv.URL + "/anthropic", "api_key": "ak",
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("自动推断 lookup: %d %v", code, env)
	}
	if m := env["data"].(map[string]any)["models"].([]any); len(m) != 2 {
		t.Fatalf("models = %v", m)
	}
}

// 子路径 BaseURL（DeepSeek 风格）自身无列表接口时，接口应降级用主域名列表。
func TestLookupModelsSubPathFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/anthropic/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid URL"}}`))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	_, api := newTestEnv(t)

	code, env, _ := doJSON(t, api, "POST", "/api/v1/channels/lookup-models", map[string]any{
		"provider": "anthropic", "base_url": srv.URL + "/anthropic", "api_key": "ak",
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("lookup: %d %v", code, env)
	}
	models := env["data"].(map[string]any)["models"].([]any)
	if len(models) != 2 || models[0] != "deepseek-chat" {
		t.Fatalf("models = %v", models)
	}
}
