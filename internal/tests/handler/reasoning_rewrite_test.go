package handler_test

import (
	"testing"

	"xtokenhub/internal/model"
)

// fullMapping 全档位映射（预置方案）：none/minimal/low -> disabled，其余 -> enabled。
func fullMapping() []map[string]any {
	return []map[string]any{
		{"level": "none", "target": "disabled"},
		{"level": "minimal", "target": "disabled"},
		{"level": "low", "target": "disabled"},
		{"level": "medium", "target": "enabled"},
		{"level": "high", "target": "enabled"},
		{"level": "xhigh", "target": "enabled"},
		{"level": "max", "target": "enabled"},
	}
}

// TestReasoningRewriteCRUD 思维档位改写规则管理接口。
func TestReasoningRewriteCRUD(t *testing.T) {
	_, srv := newTestEnv(t)

	in := map[string]any{
		"model":   "mimo-v2.6-flash",
		"mapping": fullMapping(),
		"missing": "",
		"remark":  "MiMo 只认 thinking 开关",
	}

	// 创建
	code, env, raw := doJSON(t, srv, "POST", "/api/v1/reasoning-rewrites", in)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("create: %d %s", code, raw)
	}
	id := int64(env["data"].(map[string]any)["id"].(float64))

	// 重复创建 -> 400
	code, _, _ = doJSON(t, srv, "POST", "/api/v1/reasoning-rewrites", in)
	if code != 400 {
		t.Errorf("重复创建应 400: %d", code)
	}

	// 非法目标 -> 400
	bad := map[string]any{"model": "other",
		"mapping": []map[string]any{{"level": "low", "target": "on"}}}
	code, _, _ = doJSON(t, srv, "POST", "/api/v1/reasoning-rewrites", bad)
	if code != 400 {
		t.Errorf("非法目标应 400: %d", code)
	}

	// 未知档位 -> 400
	bad = map[string]any{"model": "other",
		"mapping": []map[string]any{{"level": "ultra", "target": "enabled"}}}
	code, _, _ = doJSON(t, srv, "POST", "/api/v1/reasoning-rewrites", bad)
	if code != 400 {
		t.Errorf("未知档位应 400: %d", code)
	}

	// 列表
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/reasoning-rewrites", nil)
	if code != 200 || env["data"].(map[string]any)["total"] != float64(1) {
		t.Errorf("list: %d %v", code, env)
	}

	// 详情：mapping 数组回读
	_, env, _ = doJSON(t, srv, "GET", "/api/v1/reasoning-rewrites/"+itoa(id), nil)
	data := env["data"].(map[string]any)
	if data["model"] != "mimo-v2.6-flash" {
		t.Errorf("detail: %v", data)
	}
	mapping := data["mapping"].([]any)
	if len(mapping) != 7 {
		t.Fatalf("mapping 应有 7 档: %v", mapping)
	}
	first := mapping[0].(map[string]any)
	last := mapping[6].(map[string]any)
	if first["level"] != "none" || first["target"] != model.ReasoningOff {
		t.Errorf("首档错误: %v", first)
	}
	if last["level"] != "max" || last["target"] != model.ReasoningOn {
		t.Errorf("末档错误: %v", last)
	}

	// 更新：改 low 为开思维并注入缺省关思维
	upd := map[string]any{
		"model":   "mimo-v2.6-flash",
		"mapping": fullMapping(),
		"missing": model.ReasoningOff,
		"remark":  "",
	}
	upd["mapping"] = []map[string]any{
		{"level": "none", "target": "disabled"},
		{"level": "low", "target": "enabled"},
		{"level": "max", "target": "enabled"},
	}
	code, _, raw = doJSON(t, srv, "PUT", "/api/v1/reasoning-rewrites/"+itoa(id), upd)
	if code != 200 {
		t.Fatalf("update: %d %s", code, raw)
	}
	_, env, _ = doJSON(t, srv, "GET", "/api/v1/reasoning-rewrites/"+itoa(id), nil)
	data = env["data"].(map[string]any)
	if data["missing"] != model.ReasoningOff {
		t.Errorf("missing 未生效: %v", data)
	}
	mapping = data["mapping"].([]any)
	if len(mapping) != 3 {
		t.Fatalf("更新后应 3 档: %v", mapping)
	}
	if mapping[1].(map[string]any)["target"] != model.ReasoningOn {
		t.Errorf("low 目标未更新: %v", mapping[1])
	}

	// 删除
	code, _, _ = doJSON(t, srv, "DELETE", "/api/v1/reasoning-rewrites/"+itoa(id), nil)
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	code, _, _ = doJSON(t, srv, "GET", "/api/v1/reasoning-rewrites/"+itoa(id), nil)
	if code != 404 {
		t.Errorf("删除后 get 应 404: %d", code)
	}
}
