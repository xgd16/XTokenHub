package provider_test

import (
	"testing"

	"xtokenhub/internal/provider"
)

// samplePricing 模拟公开价格表的关键形状：样例说明键、embedding、免费模型、厂商前缀、
// 长上下文分档、以及 1 小时缓存写键（后者不应被当成阈值分档）。
const samplePricing = `{
  "sample_spec": {"input_cost_per_token": 0, "output_cost_per_token": 0, "mode": "chat"},
  "gpt-4o": {"input_cost_per_token": 0.0000025, "output_cost_per_token": 0.00001,
    "cache_read_input_token_cost": 0.00000125, "litellm_provider": "openai", "mode": "chat"},
  "claude-sonnet-4-20250514": {"input_cost_per_token": 0.000003, "output_cost_per_token": 0.000015,
    "cache_creation_input_token_cost": 0.00000375, "cache_read_input_token_cost": 0.0000003,
    "input_cost_per_token_above_200k_tokens": 0.000006,
    "output_cost_per_token_above_200k_tokens": 0.0000225,
    "cache_creation_input_token_cost_above_1hr": 0.000006,
    "litellm_provider": "anthropic", "mode": "chat"},
  "text-embedding-3-small": {"input_cost_per_token": 0.00000002, "mode": "embedding"},
  "free-model": {"input_cost_per_token": 0, "output_cost_per_token": 0, "mode": "chat"},
  "openrouter/meta/llama-3": {"input_cost_per_token": 0.000001, "mode": "chat"}
}`

func TestParsePricingFiltersAndThresholds(t *testing.T) {
	entries, err := provider.ParsePricing([]byte(samplePricing))
	if err != nil {
		t.Fatalf("解析价格表: %v", err)
	}
	idx := provider.IndexPrices(entries)

	// 说明样例、embedding 模式、无价格条目都不应进入价格表
	if _, ok := idx["sample_spec"]; ok {
		t.Error("sample_spec 不应入库")
	}
	if _, ok := idx["text-embedding-3-small"]; ok {
		t.Error("embedding 模式不应入库")
	}
	if _, ok := idx["free-model"]; ok {
		t.Error("无价格条目不应入库（应作为未定价模型提示）")
	}
	if len(entries) != 3 {
		t.Fatalf("条目数 = %d, want 3", len(entries))
	}

	claude, ok := provider.MatchPrice(idx, "claude-sonnet-4-20250514")
	if !ok {
		t.Fatal("claude 未匹配")
	}
	if claude.CacheWriteCostPerToken != 0.00000375 {
		t.Errorf("缓存写费率 = %v, want 3.75e-6（5 分钟档，不取 1 小时档）", claude.CacheWriteCostPerToken)
	}
	if claude.ThresholdTokens != 200000 {
		t.Errorf("阈值 = %d, want 200000", claude.ThresholdTokens)
	}
	if claude.InputCostAbovePerToken != 0.000006 || claude.OutputCostAbovePerToken != 0.0000225 {
		t.Errorf("超阈值费率 = (%v,%v), want (6e-6,2.25e-5)", claude.InputCostAbovePerToken, claude.OutputCostAbovePerToken)
	}
}

func TestParsePricingErrors(t *testing.T) {
	if _, err := provider.ParsePricing([]byte("not json")); err == nil {
		t.Error("非法 JSON 应报错")
	}
	if _, err := provider.ParsePricing([]byte(`{"a":{"mode":"chat"}}`)); err == nil {
		t.Error("没有任何可用条目应报错")
	}
}

func TestMatchPriceCandidates(t *testing.T) {
	entries, err := provider.ParsePricing([]byte(samplePricing))
	if err != nil {
		t.Fatal(err)
	}
	idx := provider.IndexPrices(entries)

	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-4o", true},                  // 精确
		{"GPT-4O", true},                  // 大小写不敏感
		{"openai/gpt-4o", true},           // 去厂商前缀
		{"openrouter/meta/llama-3", true}, // 精确（带斜杠的完整键）
		{"meta/llama-3", true},            // 去厂商前缀
		{"llama-3", true},                 // 去多级前缀后的末段
		{"  gpt-4o  ", true},              // 首尾空白
		{"gpt-4o-mini", false},            // 未收录
		{"", false},                       // 空模型名
	}
	for _, c := range cases {
		if _, ok := provider.MatchPrice(idx, c.model); ok != c.want {
			t.Errorf("MatchPrice(%q) = %v, want %v", c.model, ok, c.want)
		}
	}
}

// 目录里带日期版本、请求里不带时，靠「去日期后缀」别名兜住。
func TestMatchPriceDateSuffixAlias(t *testing.T) {
	entries, _ := provider.ParsePricing([]byte(samplePricing))
	idx := provider.IndexPrices(entries)
	e, ok := provider.MatchPrice(idx, "claude-sonnet-4")
	if !ok {
		t.Fatal("请求名缺日期版本时应命中带日期键的别名")
	}
	if e.CacheWriteCostPerToken != 0.00000375 {
		t.Errorf("别名命中的条目不对: %+v", e)
	}
}

// 别名不得抢占规范名：同名时裸名必须指向无前缀条目，带前缀的键仍指向自己。
func TestIndexPricesAliasPriority(t *testing.T) {
	const payload = `{
	  "gpt-4o": {"input_cost_per_token": 0.0000025, "output_cost_per_token": 0.00001, "mode": "chat"},
	  "azure/gpt-4o": {"input_cost_per_token": 0.000005, "output_cost_per_token": 0.00002, "mode": "chat"}
	}`
	entries, err := provider.ParsePricing([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	idx := provider.IndexPrices(entries)

	bare, _ := provider.MatchPrice(idx, "gpt-4o")
	if bare.InputCostPerToken != 0.0000025 {
		t.Errorf("裸名应指向无前缀条目, got %v", bare.InputCostPerToken)
	}
	azure, _ := provider.MatchPrice(idx, "azure/gpt-4o")
	if azure.InputCostPerToken != 0.000005 {
		t.Errorf("带前缀键应指向自己的条目, got %v", azure.InputCostPerToken)
	}
}
