package handler_test

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"xtokenhub/internal/model"
	"xtokenhub/internal/service"
)

// doGatewayRaw 发送网关请求（可自定义头），返回状态码与响应体。
func doGatewayRaw(t *testing.T, srvURL, path, body string, header map[string]string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srvURL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// doGateway 携带给定 Authorization 头发送网关请求。
func doGateway(t *testing.T, srvURL, path, body, authorization string) (int, []byte) {
	t.Helper()
	h := map[string]string{}
	if authorization != "" {
		h["Authorization"] = authorization
	}
	return doGatewayRaw(t, srvURL, path, body, h)
}

const chatBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`

func TestKeyAdminCRUD(t *testing.T) {
	_, srv := newTestEnv(t)

	// 创建
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/keys", map[string]any{
		"name": "claude-code", "remark": "主力",
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("create: %d %v", code, env)
	}
	data := env["data"].(map[string]any)
	raw := data["key"].(string)
	if !strings.HasPrefix(raw, "sk-xt-") || len(raw) != len("sk-xt-")+32 {
		t.Errorf("生成的 key 异常: %s", raw)
	}
	id := int64(data["id"].(float64))

	// 重名拒绝
	_, env, _ = doJSON(t, srv, "POST", "/api/v1/keys", map[string]any{"name": "claude-code"})
	if env["code"] == float64(0) {
		t.Errorf("重名应返回业务错误: %v", env)
	}

	// 列表
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/keys", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("list: %d %v", code, env)
	}
	if total := env["data"].(map[string]any)["total"].(float64); total != 1 {
		t.Errorf("total = %v", total)
	}

	// 更新（停用）
	code, env, _ = doJSON(t, srv, "PUT", "/api/v1/keys/"+strconv.FormatInt(id, 10), map[string]any{
		"name": "claude-code", "status": 0, "remark": "停用测试",
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("update: %d %v", code, env)
	}
	if status := env["data"].(map[string]any)["status"].(float64); status != 0 {
		t.Errorf("status 未更新: %v", status)
	}

	// 删除
	code, env, _ = doJSON(t, srv, "DELETE", "/api/v1/keys/"+strconv.FormatInt(id, 10), nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("delete: %d %v", code, env)
	}
	_, env, _ = doJSON(t, srv, "GET", "/api/v1/keys", nil)
	if total := env["data"].(map[string]any)["total"].(float64); total != 0 {
		t.Errorf("删除后 total = %v", total)
	}
}

// TestGatewayAuthMatrix require_key=true 时的鉴权行为矩阵。
func TestGatewayAuthMatrix(t *testing.T) {
	_, srv, keySvc := newTestEnvOpts(t, true)
	ctx := context.Background()

	k, err := keySvc.Create(ctx, &service.KeyInput{Name: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	valid := k.Key

	// 无 key -> 401（openai 错误形状）
	code, raw := doGateway(t, srv.URL, "/v1/chat/completions", chatBody, "")
	if code != http.StatusUnauthorized || !strings.Contains(string(raw), "缺少 API Key") {
		t.Errorf("无 key: %d %s", code, raw)
	}
	// 错误 key -> 401
	code, raw = doGateway(t, srv.URL, "/v1/chat/completions", chatBody, "Bearer sk-xt-bad")
	if code != http.StatusUnauthorized || !strings.Contains(string(raw), "无效的 API Key") {
		t.Errorf("坏 key: %d %s", code, raw)
	}
	// 有效 key（无渠道）-> 503：说明通过了鉴权进入执行器
	code, raw = doGateway(t, srv.URL, "/v1/chat/completions", chatBody, "Bearer "+valid)
	if code != http.StatusServiceUnavailable || !strings.Contains(string(raw), "没有可用渠道") {
		t.Errorf("有效 key: %d %s", code, raw)
	}
	// x-api-key 头同样有效
	code, _ = doGatewayRaw(t, srv.URL, "/v1/chat/completions", chatBody, map[string]string{"X-Api-Key": valid})
	if code != http.StatusServiceUnavailable {
		t.Errorf("x-api-key 应通过鉴权: %d", code)
	}
	// /v1/messages 的 401 使用 anthropic 错误形状
	code, raw = doGateway(t, srv.URL, "/v1/messages", `{"model":"claude-3","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`, "")
	if code != http.StatusUnauthorized || !strings.Contains(string(raw), `"type":"error"`) {
		t.Errorf("messages 401 形状: %d %s", code, raw)
	}
	// 停用 key -> 401
	disabled := model.KeyDisabled
	if _, err := keySvc.Update(ctx, k.ID, &service.KeyInput{Name: "agent-a", Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	code, _ = doGateway(t, srv.URL, "/v1/chat/completions", chatBody, "Bearer "+valid)
	if code != http.StatusUnauthorized {
		t.Errorf("停用 key 应 401: %d", code)
	}
}

// TestGatewayOpenMode require_key=false 时匿名可调，不因缺 key 报 401。
func TestGatewayOpenMode(t *testing.T) {
	_, srv, _ := newTestEnvOpts(t, false)
	code, raw := doGateway(t, srv.URL, "/v1/chat/completions", chatBody, "")
	if code != http.StatusServiceUnavailable || strings.Contains(string(raw), "API Key") {
		t.Errorf("开放模式不应 401: %d %s", code, raw)
	}
}

// TestKeyStampingInLogs 有效 key 的请求（即使因无渠道失败）也在日志/统计中带出 key 身份。
func TestKeyStampingInLogs(t *testing.T) {
	_, srv, keySvc := newTestEnvOpts(t, true)
	ctx := context.Background()

	k, err := keySvc.Create(ctx, &service.KeyInput{Name: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	code, _ := doGateway(t, srv.URL, "/v1/chat/completions", chatBody, "Bearer "+k.Key)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("预期 503（无渠道）: %d", code)
	}

	// 日志带 key 身份
	_, env, _ := doJSON(t, srv, "GET", "/api/v1/logs", nil)
	items := env["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["key_name"] != "agent-a" {
		t.Errorf("key_name 未记录: %v", first)
	}
	if kid := first["key_id"].(float64); kid != float64(k.ID) {
		t.Errorf("key_id 未记录: %v", first["key_id"])
	}

	// 按 key 统计
	_, env, _ = doJSON(t, srv, "GET", "/api/v1/stats/by-key", nil)
	stats := env["data"].([]any)
	if len(stats) != 1 || stats[0].(map[string]any)["name"] != "agent-a" {
		t.Errorf("by-key 统计异常: %v", env["data"])
	}
}
