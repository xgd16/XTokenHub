package provider_test

import (
	"xtokenhub/internal/provider"

	"encoding/json"
	"strings"
	"testing"

	"xtokenhub/internal/model"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestValidateRequest(t *testing.T) {
	valid := []struct {
		p    model.Protocol
		body string
	}{
		{model.ProtocolChatCompletions, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`},
		{model.ProtocolResponses, `{"model":"m","input":"hi"}`},
		{model.ProtocolMessages, `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`},
	}
	for _, tt := range valid {
		if err := provider.ValidateRequest(tt.p, []byte(tt.body)); err != nil {
			t.Errorf("provider.ValidateRequest(%s) err = %v", tt.p, err)
		}
	}
	invalid := []struct {
		p    model.Protocol
		body string
	}{
		{model.ProtocolChatCompletions, `{"model":"m"}`},
		{model.ProtocolChatCompletions, `not-json`},
		{model.ProtocolResponses, `{"model":"m"}`},
		{model.ProtocolMessages, `{"model":"m","messages":[]}`},
		{model.ProtocolMessages, `{"model":"m","messages":[{"role":"user"}]}`}, // 缺 max_tokens
		{model.Protocol("nope"), `{}`},
	}
	for _, tt := range invalid {
		if err := provider.ValidateRequest(tt.p, []byte(tt.body)); err == nil {
			t.Errorf("provider.ValidateRequest(%s, %s) 应报错", tt.p, tt.body)
		}
	}
}

func TestParseRequestOpenAI(t *testing.T) {
	body := `{"model":"gpt-4o","messages":[
		{"role":"system","content":"be nice"},
		{"role":"user","content":[{"type":"text","text":"hello"},{"type":"text","text":"world"}]},
		{"role":"assistant","content":"sure"}],
		"max_tokens":100,"temperature":0.5,"stop":["END"],"stream":true}`
	conv, params, err := provider.ParseRequest(model.ProtocolChatCompletions, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if conv.System != "be nice" || len(conv.Messages) != 2 {
		t.Errorf("conv = %+v", conv)
	}
	if conv.Messages[0].Text != "hello\nworld" {
		t.Errorf("parts 拼接错误: %q", conv.Messages[0].Text)
	}
	if params.Model != "gpt-4o" || params.MaxTokens != 100 || !params.Stream || len(params.Stop) != 1 {
		t.Errorf("params = %+v", params)
	}
}

func TestParseRequestResponses(t *testing.T) {
	// string input
	conv, params, err := provider.ParseRequest(model.ProtocolResponses, []byte(`{"model":"m","input":"hi","instructions":"sys","max_output_tokens":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if conv.System != "sys" || len(conv.Messages) != 1 || conv.Messages[0].Text != "hi" {
		t.Errorf("conv = %+v", conv)
	}
	if params.MaxTokens != 50 {
		t.Errorf("params = %+v", params)
	}

	// items input
	body := `{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"q1"}]},{"role":"assistant","content":[{"type":"output_text","text":"a1"}]}]}`
	conv2, _, err := provider.ParseRequest(model.ProtocolResponses, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(conv2.Messages) != 2 || conv2.Messages[1].Role != provider.RoleAssistant {
		t.Errorf("conv2 = %+v", conv2)
	}
}

func TestParseRequestAnthropic(t *testing.T) {
	body := `{"model":"claude","system":"be kind","messages":[{"role":"user","content":[{"type":"text","text":"q"}]}],"max_tokens":64}`
	conv, params, err := provider.ParseRequest(model.ProtocolMessages, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if conv.System != "be kind" || params.MaxTokens != 64 {
		t.Errorf("conv=%+v params=%+v", conv, params)
	}
}

// ---------- 上游请求序列化 ----------

func TestWriteUpstreamRequests(t *testing.T) {
	conv := provider.Conversation{
		System:   "sys",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "q"}, {Role: provider.RoleAssistant, Text: "a"}},
	}
	params := provider.ReqParams{MaxTokens: 77, Stream: true, Stop: []string{"X", "Y"}}

	// openai 上游
	b, err := provider.WriteUpstreamRequest(model.ProtocolChatCompletions, conv, params, "gpt-x")
	if err != nil {
		t.Fatal(err)
	}
	var oreq map[string]any
	_ = json.Unmarshal(b, &oreq)
	if oreq["model"] != "gpt-x" || oreq["stream"] != true {
		t.Errorf("openai 上游请求: %s", b)
	}
	opts, _ := oreq["stream_options"].(map[string]any)
	if opts == nil || opts["include_usage"] != true {
		t.Error("流式必须带 stream_options.include_usage")
	}
	msgs := oreq["messages"].([]any)
	if len(msgs) != 3 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("messages 错误: %v", msgs)
	}

	// anthropic 上游
	b2, err := provider.WriteUpstreamRequest(model.ProtocolMessages, conv, params, "claude-x")
	if err != nil {
		t.Fatal(err)
	}
	var areq map[string]any
	_ = json.Unmarshal(b2, &areq)
	if areq["model"] != "claude-x" || areq["max_tokens"] != float64(77) || areq["system"] != "sys" {
		t.Errorf("anthropic 上游请求: %s", b2)
	}
	amsgs := areq["messages"].([]any)
	first := amsgs[0].(map[string]any)
	content := first["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" || content["text"] != "q" {
		t.Errorf("anthropic content 错误: %v", content)
	}

	// anthropic 上游：max_tokens 缺省补 1024
	b3, _ := provider.WriteUpstreamRequest(model.ProtocolMessages, conv, provider.ReqParams{Stream: true}, "m")
	var areq3 map[string]any
	_ = json.Unmarshal(b3, &areq3)
	if areq3["max_tokens"] != float64(1024) {
		t.Errorf("max_tokens 默认值错误: %v", areq3["max_tokens"])
	}

	// responses 上游不支持转换写入
	if _, err := provider.WriteUpstreamRequest(model.ProtocolResponses, conv, params, "m"); err == nil {
		t.Error("responses 上游转换应报错")
	}
}

// ---------- 上游响应解析 + 客户端响应序列化 ----------

func TestConvertResponseMatrix(t *testing.T) {
	// openai 上游响应 -> 三种客户端协议
	openaiResp := mustJSON(t, map[string]any{
		"id": "chatcmpl-1", "model": "gpt-x",
		"choices": []map[string]any{{
			"message":       map[string]any{"role": "assistant", "content": "answer"},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 7, "total_tokens": 19, "prompt_tokens_details": map[string]any{"cached_tokens": 6}},
	})
	res, err := provider.ParseUpstreamResponse(model.ProtocolChatCompletions, openaiResp)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "answer" || res.Usage.PromptTokens != 12 || res.Usage.CachedTokens != 6 {
		t.Errorf("openai 上游解析: %+v", res)
	}

	// -> 客户端 chat/completions（原生，等价回放）
	b1, err := provider.WriteClientResponse(model.ProtocolChatCompletions, res)
	if err != nil || !strings.Contains(string(b1), `"total_tokens":19`) {
		t.Errorf("chat 客户端响应: %s err=%v", b1, err)
	}
	// -> 客户端 messages
	b2, err := provider.WriteClientResponse(model.ProtocolMessages, res)
	if err != nil {
		t.Fatal(err)
	}
	var am map[string]any
	_ = json.Unmarshal(b2, &am)
	usage := am["usage"].(map[string]any)
	if usage["input_tokens"] != float64(12) || usage["output_tokens"] != float64(7) {
		t.Errorf("messages 客户端 usage: %v", usage)
	}
	content := am["content"].([]any)[0].(map[string]any)
	if content["text"] != "answer" {
		t.Errorf("messages 内容: %v", content)
	}
	// -> 客户端 responses
	b3, err := provider.WriteClientResponse(model.ProtocolResponses, res)
	if err != nil {
		t.Fatal(err)
	}
	var rm map[string]any
	_ = json.Unmarshal(b3, &rm)
	if rm["object"] != "response" || rm["status"] != "completed" {
		t.Errorf("responses 客户端响应: %s", b3)
	}

	// anthropic 上游响应 -> 客户端 chat/completions
	anthropicResp := mustJSON(t, map[string]any{
		"id": "msg-1", "model": "claude-x", "stop_reason": "end_turn",
		"content": []map[string]any{{"type": "text", "text": "bonjour"}},
		"usage":   map[string]any{"input_tokens": 33, "output_tokens": 8, "cache_read_input_tokens": 11},
	})
	res2, err := provider.ParseUpstreamResponse(model.ProtocolMessages, anthropicResp)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Text != "bonjour" || res2.Usage.CachedTokens != 11 {
		t.Errorf("anthropic 上游解析: %+v", res2)
	}
	b4, err := provider.WriteClientResponse(model.ProtocolChatCompletions, res2)
	if err != nil {
		t.Fatal(err)
	}
	var cm map[string]any
	_ = json.Unmarshal(b4, &cm)
	cu := cm["usage"].(map[string]any)
	if cu["prompt_tokens"] != float64(33) {
		t.Errorf("chat 客户端 usage: %v", cu)
	}
}

func TestParseUpstreamResponseErrors(t *testing.T) {
	if _, err := provider.ParseUpstreamResponse(model.ProtocolChatCompletions, []byte(`broken`)); err == nil {
		t.Error("非法 JSON 应报错")
	}
	if _, err := provider.ParseUpstreamResponse(model.ProtocolResponses, []byte(`{}`)); err == nil {
		t.Error("responses 上游不支持解析应报错")
	}
}

// ---------- 请求端到端转换（入站协议 -> 上游协议） ----------

func TestConvertRequestMatrix(t *testing.T) {
	samples := map[model.Protocol][]byte{
		model.ProtocolChatCompletions: []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}],"max_tokens":20}`),
		model.ProtocolMessages:        []byte(`{"model":"claude-3","system":"s","messages":[{"role":"user","content":"hi"}],"max_tokens":20}`),
		model.ProtocolResponses:       []byte(`{"model":"gpt-4o","input":"hi","instructions":"s","max_output_tokens":20}`),
	}

	// CC 入站 -> anthropic 上游
	up, err := provider.ConvertRequest(model.ProtocolChatCompletions, model.ProtocolMessages, samples[model.ProtocolChatCompletions], "claude-3-5")
	if err != nil {
		t.Fatal(err)
	}
	var am map[string]any
	_ = json.Unmarshal(up, &am)
	if am["model"] != "claude-3-5" || am["system"] != "s" || am["max_tokens"] != float64(20) {
		t.Errorf("CC->MS: %s", up)
	}

	// MS 入站 -> openai 上游
	up2, err := provider.ConvertRequest(model.ProtocolMessages, model.ProtocolChatCompletions, samples[model.ProtocolMessages], "gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	var om map[string]any
	_ = json.Unmarshal(up2, &om)
	msgs := om["messages"].([]any)
	if om["model"] != "gpt-4o-mini" || len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("MS->CC: %s", up2)
	}

	// RS 入站 -> openai 上游
	up3, err := provider.ConvertRequest(model.ProtocolResponses, model.ProtocolChatCompletions, samples[model.ProtocolResponses], "gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	var om2 map[string]any
	_ = json.Unmarshal(up3, &om2)
	if om2["model"] != "gpt-4o-mini" || om2["max_tokens"] != float64(20) {
		t.Errorf("RS->CC: %s", up3)
	}

	// RS 入站 -> anthropic 上游
	up4, err := provider.ConvertRequest(model.ProtocolResponses, model.ProtocolMessages, samples[model.ProtocolResponses], "claude-3-5")
	if err != nil {
		t.Fatal(err)
	}
	var am2 map[string]any
	_ = json.Unmarshal(up4, &am2)
	if am2["system"] != "s" || am2["max_tokens"] != float64(20) {
		t.Errorf("RS->MS: %s", up4)
	}

	// 原生相同协议：不应走转换
	if _, err := provider.ConvertRequest(model.ProtocolMessages, model.ProtocolMessages, samples[model.ProtocolMessages], "m"); err == nil {
		t.Error("相同协议应拒绝转换（应走透传）")
	}
}
