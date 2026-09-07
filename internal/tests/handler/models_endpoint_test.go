package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"xtokenhub/internal/model"
)

// TestGatewayModelsEndpoint GET /v1/models 聚合启用渠道模型清单（去重排序）。
func TestGatewayModelsEndpoint(t *testing.T) {
	_, srv := newTestEnv(t)

	for _, ch := range []struct {
		name   string
		models []string
	}{
		{"ch-a", []string{"gpt-4o", "deepseek-chat"}},
		{"ch-b", []string{"deepseek-chat", "claude-sonnet-4-5"}},
	} {
		code, env, _ := doJSON(t, srv, "POST", "/api/v1/channels", map[string]any{
			"name": ch.name, "base_url": "https://up.example.com", "api_key": "k",
			"models": ch.models,
		})
		if code != 200 || env["code"] != float64(0) {
			t.Fatalf("create %s: %d %v", ch.name, code, env)
		}
	}

	resp, err := http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Object string `json:"object"`
		First  any    `json:"first_id"`
		Last   any    `json:"last_id"`
		HasMore bool  `json:"has_more"`
		Data   []struct {
			ID          string `json:"id"`
			Object      string `json:"object"`
			Type        string `json:"type"`
			DisplayName string `json:"display_name"`
			OwnedBy     string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" {
		t.Errorf("object = %q", body.Object)
	}
	want := []string{"claude-sonnet-4-5", "deepseek-chat", "gpt-4o"}
	if len(body.Data) != len(want) {
		t.Fatalf("data = %+v", body.Data)
	}
	for i, id := range want {
		e := body.Data[i]
		if e.ID != id {
			t.Errorf("data[%d].id = %q, want %q", i, e.ID, id)
		}
		if e.Object != "model" || e.Type != "model" || e.DisplayName != id || e.OwnedBy != "xtokenhub" {
			t.Errorf("data[%d] 字段不符: %+v", i, e)
		}
	}
	if body.First != want[0] || body.Last != want[len(want)-1] || body.HasMore {
		t.Errorf("分页字段: %v %v %v", body.First, body.Last, body.HasMore)
	}
}

// TestGatewayModelsRequireKey require_key 开启时 /v1/models 同样受网关密钥保护。
func TestGatewayModelsRequireKey(t *testing.T) {
	_, srv, _ := newTestEnvOpts(t, true)

	// 无密钥 -> 401
	resp, err := http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无密钥应 401, got %d", resp.StatusCode)
	}

	// 创建密钥后 -> 200
	_, env, _ := doJSON(t, srv, "POST", "/api/v1/keys", map[string]any{"name": "k1"})
	raw := env["data"].(map[string]any)["key"].(string)
	if len(raw) < len(model.KeyPrefix) {
		t.Fatalf("密钥响应缺原文: %v", env)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("带密钥应 200, got %d", resp2.StatusCode)
	}
}
