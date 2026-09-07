package provider_test

import (
	"xtokenhub/internal/provider"

	"encoding/json"
	"strings"
	"testing"

	"xtokenhub/internal/model"
)

// ---------- 非流式 usage 解析 ----------

func TestParseUsageOpenAIChat(t *testing.T) {
	body := map[string]any{
		"usage": map[string]any{
			"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150,
			"prompt_tokens_details": map[string]any{"cached_tokens": 80},
		},
	}
	raw, _ := json.Marshal(body)
	u, ok := provider.ParseUsage(model.ProtocolChatCompletions, raw)
	if !ok {
		t.Fatal("应解析成功")
	}
	if u.PromptTokens != 100 || u.CompletionTokens != 50 || u.TotalTokens != 150 || u.CachedTokens != 80 {
		t.Errorf("usage = %+v", u)
	}
	if u.Source != provider.UsageFromUpstream {
		t.Errorf("source = %s", u.Source)
	}
}

func TestParseUsageResponses(t *testing.T) {
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens": 30, "output_tokens": 20,
			"input_tokens_details": map[string]any{"cached_tokens": 10},
		},
	}
	raw, _ := json.Marshal(body)
	u, ok := provider.ParseUsage(model.ProtocolResponses, raw)
	if !ok || u.PromptTokens != 30 || u.CompletionTokens != 20 || u.CachedTokens != 10 {
		t.Errorf("usage = %+v ok=%v", u, ok)
	}
	if u.TotalTokens != 50 {
		t.Errorf("total 未补全: %d", u.TotalTokens)
	}
}

func TestParseUsageAnthropic(t *testing.T) {
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens": 40, "output_tokens": 60,
			"cache_creation_input_tokens": 25, "cache_read_input_tokens": 35,
		},
	}
	raw, _ := json.Marshal(body)
	u, ok := provider.ParseUsage(model.ProtocolMessages, raw)
	if !ok || u.PromptTokens != 40 || u.CompletionTokens != 60 || u.CacheWriteTokens != 25 || u.CachedTokens != 35 {
		t.Errorf("usage = %+v ok=%v", u, ok)
	}
}

func TestParseUsageInvalid(t *testing.T) {
	if _, ok := provider.ParseUsage(model.ProtocolChatCompletions, []byte(`{"error":"x"}`)); ok {
		t.Error("无 usage 应返回 false")
	}
	if _, ok := provider.ParseUsage(model.ProtocolChatCompletions, []byte(`not-json`)); ok {
		t.Error("非法 JSON 应返回 false")
	}
}

// ---------- 流式解析器 ----------

func TestOpenAIStreamParserWithUsage(t *testing.T) {
	p := provider.NewStreamParser(model.ProtocolChatCompletions)
	chunks := []string{
		`{"choices":[{"delta":{"content":"你"}}]}`,
		`{"choices":[{"delta":{"content":"好"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":4}}}`,
	}
	for _, c := range chunks {
		if err := p.Feed([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	u, ok := p.Usage()
	if !ok {
		t.Fatal("应报告 usage")
	}
	if u.PromptTokens != 10 || u.CompletionTokens != 2 || u.CachedTokens != 4 || u.TotalTokens != 12 {
		t.Errorf("usage = %+v", u)
	}
	if p.Text() != "你好" {
		t.Errorf("text = %q", p.Text())
	}
}

func TestResponsesStreamParser(t *testing.T) {
	p := provider.NewStreamParser(model.ProtocolResponses)
	for _, c := range []string{
		`{"type":"response.created"}`,
		`{"type":"response.output_text.delta","delta":"he"}`,
		`{"type":"response.output_text.delta","delta":"llo"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":7,"output_tokens":3}}}`,
	} {
		if err := p.Feed([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	u, ok := p.Usage()
	if !ok || u.PromptTokens != 7 || u.CompletionTokens != 3 {
		t.Errorf("usage = %+v ok=%v", u, ok)
	}
	if p.Text() != "hello" {
		t.Errorf("text = %q", p.Text())
	}
}

func TestAnthropicStreamParser(t *testing.T) {
	p := provider.NewStreamParser(model.ProtocolMessages)
	for _, c := range []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":60,"cache_creation_input_tokens":5}}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"héllo "}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"世界"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`,
		`{"type":"message_stop"}`,
	} {
		if err := p.Feed([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	u, ok := p.Usage()
	if !ok {
		t.Fatal("应报告 usage")
	}
	if u.PromptTokens != 100 || u.CompletionTokens != 8 || u.CachedTokens != 60 || u.CacheWriteTokens != 5 || u.TotalTokens != 108 {
		t.Errorf("usage = %+v", u)
	}
	if !strings.Contains(p.Text(), "世界") {
		t.Errorf("text = %q", p.Text())
	}
}

// ---------- 本地估算 ----------

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		min   int64
		max   int64
	}{
		{"空", "", 0, 0},
		{"英文短句", "hello world this is a test", 5, 15},
		{"中文", "这是一段中文文本用于测试token估算", 8, 30},
		{"混合", "hello 你好", 3, 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.EstimateTokens(tt.input)
			if got < tt.min || got > tt.max {
				t.Errorf("provider.EstimateTokens(%q) = %d, want [%d,%d]", tt.input, got, tt.min, tt.max)
			}
		})
	}
}

func TestEstimateBytesInvalidUTF8(t *testing.T) {
	got := provider.EstimateBytes([]byte{0xff, 0xfe, 0x00, 0x01})
	if got <= 0 {
		t.Errorf("非法 UTF-8 也应有粗略值: %d", got)
	}
}
