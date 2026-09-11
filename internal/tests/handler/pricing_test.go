package handler_test

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/repository"
)

// 价格表：默认状态带同步信息与未定价提示。
func TestAdminPriceList(t *testing.T) {
	_, srv := newTestEnv(t)

	code, env, _ := doJSON(t, srv, "GET", "/api/v1/settings/prices?used_only=0", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("list: %d %v", code, env)
	}
	data := env["data"].(map[string]any)
	if data["total"].(float64) < 2 {
		t.Errorf("价格表行数 = %v, want >= 2（EnsureDefaults 已同步桩源）", data["total"])
	}
	status := data["status"].(map[string]any)
	if status["enabled"] != true {
		t.Errorf("status.enabled = %v", status["enabled"])
	}
	if status["price_count"].(float64) < 2 {
		t.Errorf("price_count = %v", status["price_count"])
	}
	if _, ok := data["billing"]; !ok {
		t.Error("响应应附带计费设置")
	}
	if _, ok := data["unpriced"]; !ok {
		t.Error("响应应附带未定价模型列表")
	}
}

// 手工价格的新增 / 编辑（转为手工）/ 删除 / 参数校验。
func TestAdminPriceCRUD(t *testing.T) {
	_, srv := newTestEnv(t)

	// 模型名为空 -> 400
	code, _, _ := doJSON(t, srv, "POST", "/api/v1/settings/prices", map[string]any{
		"model": "", "input_cost_per_token": 0.000001,
	})
	if code != 400 {
		t.Errorf("空模型名应 400, got %d", code)
	}

	// 新增
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/settings/prices", map[string]any{
		"model": "my-own-model", "input_cost_per_token": 0.000001, "output_cost_per_token": 0.000002,
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("create: %d %v", code, env)
	}
	created := env["data"].(map[string]any)
	id := int64(created["id"].(float64))
	if created["source"] != "manual" {
		t.Errorf("手工新增 source = %v", created["source"])
	}

	// 重复新增 -> 400
	code, _, _ = doJSON(t, srv, "POST", "/api/v1/settings/prices", map[string]any{
		"model": "my-own-model", "input_cost_per_token": 0.000001,
	})
	if code != 400 {
		t.Errorf("重复模型应 400, got %d", code)
	}

	// 编辑：改成 manual 并生效
	code, env, _ = doJSON(t, srv, "PUT", "/api/v1/settings/prices/"+itoa(id), map[string]any{
		"input_cost_per_token": 0.000009, "output_cost_per_token": 0.00009,
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("update: %d %v", code, env)
	}
	if env["data"].(map[string]any)["input_cost_per_token"] != 0.000009 {
		t.Errorf("更新未生效: %v", env["data"])
	}

	// 非法 id
	code, _, _ = doJSON(t, srv, "PUT", "/api/v1/settings/prices/99999", map[string]any{
		"input_cost_per_token": 0.000001,
	})
	if code != 404 {
		t.Errorf("不存在的 id 应 404, got %d", code)
	}

	// 删除
	code, _, _ = doJSON(t, srv, "DELETE", "/api/v1/settings/prices/"+itoa(id), nil)
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/settings/prices?q=my-own-model&used_only=0", nil)
	if code != 200 {
		t.Fatalf("list after delete: %d", code)
	}
	if env["data"].(map[string]any)["total"].(float64) != 0 {
		t.Errorf("删除后仍能查到: %v", env["data"])
	}
}

// 搜索与分页。
func TestAdminPriceListQuery(t *testing.T) {
	_, srv := newTestEnv(t)

	code, env, _ := doJSON(t, srv, "GET", "/api/v1/settings/prices?q=SONNET&used_only=0", nil)
	if code != 200 {
		t.Fatalf("search: %d", code)
	}
	if env["data"].(map[string]any)["total"].(float64) != 1 {
		t.Errorf("模糊搜索（大小写不敏感）结果 = %v", env["data"].(map[string]any)["total"])
	}

	code, env, _ = doJSON(t, srv, "GET", "/api/v1/settings/prices?q=nope-xyz&used_only=0", nil)
	if code != 200 || env["data"].(map[string]any)["total"].(float64) != 0 {
		t.Errorf("无匹配搜索应返回 0 条: %v", env["data"])
	}

	// 分页：每页 1 条
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/settings/prices?page=1&per_page=1&used_only=0", nil)
	if code != 200 {
		t.Fatalf("paged: %d", code)
	}
	page := env["data"].(map[string]any)
	if len(page["items"].([]any)) != 1 {
		t.Errorf("每页条数 = %d, want 1", len(page["items"].([]any)))
	}

	// 通配符输入不应被当成 LIKE 通配（转义后按字面匹配 -> 0 条）
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/settings/prices?q=%25&used_only=0", nil)
	if code != 200 || env["data"].(map[string]any)["total"].(float64) != 0 {
		t.Errorf("%% 应按字面匹配而非通配: %v", env["data"])
	}
}

// 同步接口：走桩源写入价格表。
func TestAdminPriceSync(t *testing.T) {
	_, srv := newTestEnv(t)
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/settings/prices/sync", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("sync: %d %v", code, env)
	}
	data := env["data"].(map[string]any)
	if data["written"].(float64) < 2 {
		t.Errorf("写入行数 = %v, want >= 2", data["written"])
	}
}

// 计费设置读写与校验。
func TestAdminBilling(t *testing.T) {
	_, srv := newTestEnv(t)

	code, env, _ := doJSON(t, srv, "GET", "/api/v1/settings/billing", nil)
	if code != 200 {
		t.Fatalf("get billing: %d", code)
	}
	if env["data"].(map[string]any)["display_currency"] != "USD" {
		t.Errorf("默认币种 = %v", env["data"])
	}

	// CNY 缺汇率 -> 400
	code, _, _ = doJSON(t, srv, "PUT", "/api/v1/settings/billing", map[string]any{
		"display_currency": "CNY", "usd_cny_rate": 0,
	})
	if code != 400 {
		t.Errorf("CNY 缺汇率应 400, got %d", code)
	}

	code, env, _ = doJSON(t, srv, "PUT", "/api/v1/settings/billing", map[string]any{
		"display_currency": "CNY", "usd_cny_rate": 7.15, "monthly_budget_usd": 50,
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("put billing: %d %v", code, env)
	}
	d := env["data"].(map[string]any)
	if d["display_currency"] != "CNY" || d["usd_cny_rate"] != 7.15 || d["monthly_budget_usd"] != float64(50) {
		t.Errorf("保存结果 = %v", d)
	}
}

// 花费预测：无数据时不给预测但说明原因；非法周期报错。
func TestAdminCostForecast(t *testing.T) {
	_, srv := newTestEnv(t)

	code, env, _ := doJSON(t, srv, "GET", "/api/v1/stats/cost/forecast?period=today", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("forecast: %d %v", code, env)
	}
	d := env["data"].(map[string]any)
	if d["period"] != "today" {
		t.Errorf("period = %v", d["period"])
	}
	if d["spent_usd"].(float64) != 0 {
		t.Errorf("无消费时 spent = %v", d["spent_usd"])
	}
	if d["projected_usd"] != nil {
		t.Errorf("无消费时不应给预测: %v", d["projected_usd"])
	}
	if d["reason"] == "" || d["reason"] == nil {
		t.Error("应说明无法预测的原因")
	}

	code, _, _ = doJSON(t, srv, "GET", "/api/v1/stats/cost/forecast?period=week", nil)
	if code != 400 {
		t.Errorf("非法 period 应 400, got %d", code)
	}
}

// 未定价模型与费用重算接口。
func TestAdminCostUnpricedAndRecompute(t *testing.T) {
	db, _, srv := newTestEnvWithDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	if err := logRepo.Create(context.Background(), &model.RequestLog{
		Model: "mystery-model", Protocol: model.ProtocolChatCompletions,
		ForwardMode: model.ForwardNativePassthrough, ChannelID: 1,
		PromptTokens: 500, CompletionTokens: 100, TotalTokens: 600,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	code, env, _ := doJSON(t, srv, "GET", "/api/v1/stats/cost/unpriced?hours=24", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("unpriced: %d %v", code, env)
	}
	list := env["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("未定价模型 = %v, want 1 条", list)
	}
	if list[0].(map[string]any)["model"] != "mystery-model" {
		t.Errorf("未定价模型名 = %v", list[0])
	}

	// 重算：未定价模型费用仍为 0，但口径会被写入
	code, env, _ = doJSON(t, srv, "POST", "/api/v1/stats/cost/recompute", map[string]any{"only_missing": true})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("recompute: %d %v", code, env)
	}
	d := env["data"].(map[string]any)
	if d["scanned"].(float64) != 1 {
		t.Errorf("扫描 = %v, want 1", d["scanned"])
	}

	// 已定价模型（桩源里的 gpt-4o）能算出费用
	if err := logRepo.Create(context.Background(), &model.RequestLog{
		Model: "gpt-4o", Protocol: model.ProtocolChatCompletions,
		ForwardMode: model.ForwardNativePassthrough, ChannelID: 1,
		PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := doJSON(t, srv, "POST", "/api/v1/stats/cost/recompute", map[string]any{"only_missing": true}); code != 200 {
		t.Fatalf("recompute 第二次: %d", code)
	}
	logs, _, err := logRepo.List(context.Background(), repository.LogFilter{Model: "gpt-4o"},
		pagination.Params{Page: 1, PerPage: 10, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("日志条数 = %d", len(logs))
	}
	// 100*2.5e-6 + 50*1e-5 = 2.5e-4 + 5e-4 = 7.5e-4
	if want := 0.00075; logs[0].CostUSD != want {
		t.Errorf("重算费用 = %v, want %v", logs[0].CostUSD, want)
	}
	if logs[0].UsageStyle != model.UsageStyleOpenAI {
		t.Errorf("口径 = %q, want openai", logs[0].UsageStyle)
	}
}

// 人民币 + 时段价通过接口往返，且非法币种/非法时段规则被拒。
func TestAdminPriceCurrencyAndPeakWindow(t *testing.T) {
	_, srv := newTestEnv(t)

	// 非法币种 -> 400
	code, _, _ := doJSON(t, srv, "POST", "/api/v1/settings/prices", map[string]any{
		"model": "cn-bad", "currency": "JPY", "input_cost_per_token": 1e-6,
	})
	if code != 400 {
		t.Errorf("非法币种应 400, got %d", code)
	}

	// 非法时段规则 -> 400
	code, _, _ = doJSON(t, srv, "POST", "/api/v1/settings/prices", map[string]any{
		"model": "cn-bad2", "input_cost_per_token": 1e-6, "peak_window": "1-5",
	})
	if code != 400 {
		t.Errorf("非法时段规则应 400, got %d", code)
	}

	// 合法：人民币 + DeepSeek 时段 + 空闲费率
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/settings/prices", map[string]any{
		"model":                          "deepseek-flash",
		"currency":                       "CNY",
		"input_cost_per_token":           2e-6,
		"output_cost_per_token":          8e-6,
		"peak_window":                    "1-5;09:00-12:00,14:00-18:00",
		"off_peak_input_cost_per_token":  1e-6,
		"off_peak_output_cost_per_token": 4e-6,
	})
	if code != 200 {
		t.Fatalf("创建人民币时段价: %d %v", code, env)
	}
	data := env["data"].(map[string]any)
	if data["currency"] != "CNY" {
		t.Errorf("currency = %v, want CNY", data["currency"])
	}
	if data["peak_window"] != "1-5;09:00-12:00,14:00-18:00" {
		t.Errorf("peak_window = %v", data["peak_window"])
	}
	if data["off_peak_input_cost_per_token"] != 1e-6 {
		t.Errorf("空闲费率 = %v, want 1e-6", data["off_peak_input_cost_per_token"])
	}

	// 列表回读同样带这些字段
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/settings/prices?q=deepseek-flash", nil)
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	items := env["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	row := items[0].(map[string]any)
	if row["currency"] != "CNY" || row["peak_window"] == "" {
		t.Errorf("列表未返回币种/时段: %v", row)
	}
}
