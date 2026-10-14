package handler_test

import (
	"testing"
)

// 节假日年历接口：查询 + 手动同步 + 参数校验。
func TestAdminHolidays(t *testing.T) {
	_, srv := newTestEnv(t)

	// 指定年份查询（远古年份，保证空表）
	code, env, _ := doJSON(t, srv, "GET", "/api/v1/settings/holidays?year=2001", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("list: %d %v", code, env)
	}
	data := env["data"].(map[string]any)
	if data["year"].(float64) != 2001 {
		t.Errorf("year = %v, want 2001", data["year"])
	}
	if items := data["items"].([]any); len(items) != 0 {
		t.Errorf("items = %v, want 空", items)
	}
	status := data["status"].(map[string]any)
	if status["enabled"] != true {
		t.Errorf("status.enabled = %v", status["enabled"])
	}

	// 手动同步（桩源无数据，写 0 行但返回成功）
	code, env, _ = doJSON(t, srv, "POST", "/api/v1/settings/holidays/sync", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("sync: %d %v", code, env)
	}
	if res := env["data"].(map[string]any); res["written"].(float64) != 0 {
		t.Errorf("written = %v, want 0（桩源空数据）", res["written"])
	}

	// 非法年份 -> 400
	code, _, _ = doJSON(t, srv, "GET", "/api/v1/settings/holidays?year=abc", nil)
	if code != 400 {
		t.Errorf("非法 year 应 400, got %d", code)
	}
}
