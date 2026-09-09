package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"xtokenhub/internal/model"
)

// ---------- usage 归一化 ----------

func TestUsageNormalize(t *testing.T) {
	u := Usage{PromptTokens: 3, CompletionTokens: 4}.Normalize()
	if u.TotalTokens != 7 {
		t.Errorf("Normalize total = %d", u.TotalTokens)
	}
	// 已有 total 不覆盖
	u2 := Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 99}.Normalize()
	if u2.TotalTokens != 99 {
		t.Errorf("Normalize 应保留已有 total")
	}
}

func TestUpstreamUsageToUsageBranches(t *testing.T) {
	// openai chat 字段分支
	u := upstreamUsage{PromptTokens: i64(1), CompletionTokens: i64(2), TotalTokens: i64(3),
		PromptDetails: &struct {
			CachedTokens *int64 `json:"cached_tokens"`
		}{CachedTokens: i64(5)}}
	got := u.toUsage()
	if got.PromptTokens != 1 || got.CompletionTokens != 2 || got.CachedTokens != 5 {
		t.Errorf("openai 分支 = %+v", got)
	}
	// responses/messages 字段分支
	u2 := upstreamUsage{InputTokens: i64(10), OutputTokens: i64(6),
		CacheRead: i64(4), CacheCreation: i64(2)}
	got2 := u2.toUsage()
	if got2.PromptTokens != 10 || got2.CompletionTokens != 6 ||
		got2.CachedTokens != 4 || got2.CacheWriteTokens != 2 {
		t.Errorf("responses 分支 = %+v", got2)
	}
	// 两分支都缺：零值
	empty := upstreamUsage{}
	got3 := empty.toUsage()
	if got3.PromptTokens != 0 || got3.Source != UsageFromUpstream {
		t.Errorf("空 usage = %+v", got3)
	}
}

func i64(v int64) *int64 { return &v }

func TestParseUsage(t *testing.T) {
	u, ok := ParseUsage(model.ProtocolChatCompletions,
		[]byte(`{"usage":{"prompt_tokens":5,"completion_tokens":2}}`))
	if !ok || u.PromptTokens != 5 || u.CompletionTokens != 2 {
		t.Errorf("usage = %+v ok = %v", u, ok)
	}
	// 无 usage 字段
	if _, ok := ParseUsage(model.ProtocolChatCompletions, []byte(`{}`)); ok {
		t.Errorf("无 usage 应返回 false")
	}
	// usage 全零
	if _, ok := ParseUsage(model.ProtocolChatCompletions,
		[]byte(`{"usage":{"prompt_tokens":0,"completion_tokens":0}}`)); ok {
		t.Errorf("全零 usage 应返回 false")
	}
	// 非法 JSON
	if _, ok := ParseUsage(model.ProtocolChatCompletions, []byte(`{bad`)); ok {
		t.Errorf("非法 JSON 应返回 false")
	}
}

func TestExtractErrorDetail(t *testing.T) {
	// 标准 error.message
	if got := ExtractErrorDetail([]byte(`{"error":{"message":"boom","type":"api_error"}}`)); got != "boom" {
		t.Errorf("detail = %q", got)
	}
	// error 为字符串
	if got := ExtractErrorDetail([]byte(`{"error":"plain"}`)); got != `"plain"` {
		t.Errorf("string error = %q", got)
	}
	// 无 error 字段：原文返回
	if got := ExtractErrorDetail([]byte(`{"foo":1}`)); got != `{"foo":1}` {
		t.Errorf("no error = %q", got)
	}
	// 非法 JSON：原文返回
	if got := ExtractErrorDetail([]byte(`{bad`)); got != `{bad` {
		t.Errorf("bad json = %q", got)
	}
	// 超长无 error：截断 512
	long := strings.Repeat("x", 600)
	got := ExtractErrorDetail([]byte(long))
	if len(got) != 512 {
		t.Errorf("长文本应截断为 512, got %d", len(got))
	}
	// error.message 为空：error 原文返回
	if got := ExtractErrorDetail([]byte(`{"error":{"code":42}}`)); got != `{"code":42}` {
		t.Errorf("empty message = %q", got)
	}
}

// ---------- StreamConverter 构造 ----------

func TestNewStreamConverterErrors(t *testing.T) {
	if _, err := NewStreamConverter(model.ProtocolChatCompletions, model.ProtocolChatCompletions); err == nil {
		t.Fatal("同协议应拒绝")
	}
	if _, err := NewStreamConverter(model.Protocol("xxx"), model.ProtocolMessages); err == nil {
		t.Fatal("未知客户端协议应拒绝")
	}
	for _, in := range []model.Protocol{model.ProtocolChatCompletions, model.ProtocolMessages, model.ProtocolResponses} {
		for _, up := range model.AllProtocols {
			if in == up {
				continue
			}
			c, err := NewStreamConverter(in, up)
			if err != nil {
				t.Fatalf("%s→%s: %v", in, up, err)
			}
			if c.Text() != "" {
				t.Fatalf("初始 Text 应为空")
			}
		}
	}
}

// ---------- 客户端: chat/completions ----------

// feedAll 喂入并聚合最终文本与事件。
func feedAll(t *testing.T, in, up model.Protocol, upstreamEvents []string) (string, [][]byte, Usage) {
	t.Helper()
	c, err := NewStreamConverter(in, up)
	if err != nil {
		t.Fatal(err)
	}
	var all [][]byte
	for _, ev := range upstreamEvents {
		out, err := c.Feed([]byte(ev))
		if err != nil {
			t.Fatalf("Feed(%s): %v", ev, err)
		}
		all = append(all, out...)
	}
	done, usage := c.Finalize()
	all = append(all, done...)
	return c.Text(), all, usage
}

func TestChatConverterFromOpenAIUpstream(t *testing.T) {
	// chat 客户端 + openai 上游是同协议（原生透传），这里直接构造转换器验证流式重组
	c := &chatChunkConverter{parser: NewStreamParser(model.ProtocolChatCompletions)}
	events := []string{
		`{"id":"chatcmpl-1","model":"up-gpt","choices":[{"index":0,"delta":{"role":"assistant","content":"He"}}]}`,
		`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"llo"}}]}`,
		`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11}}`,
	}
	var out [][]byte
	for _, ev := range events {
		o, err := c.Feed([]byte(ev))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, o...)
	}
	done, usage := c.Finalize()
	out = append(out, done...)
	if c.Text() != "Hello" {
		t.Fatalf("text = %q", c.Text())
	}
	if usage.PromptTokens != 9 || usage.CompletionTokens != 2 || usage.TotalTokens != 11 {
		t.Errorf("usage = %+v", usage)
	}
	// 首块应携带上游 id/model
	var first map[string]any
	json.Unmarshal(out[0], &first)
	if first["id"] != "chatcmpl-1" || first["model"] != "up-gpt" || first["object"] != "chat.completion.chunk" {
		t.Errorf("首块 = %v", first)
	}
	// 结尾：finish 块 + usage 块 + [DONE]
	var finish map[string]any
	json.Unmarshal(out[len(out)-3], &finish)
	ch := finish["choices"].([]any)[0].(map[string]any)
	if ch["finish_reason"] != "stop" {
		t.Errorf("finish = %v", ch)
	}
	var usageBlk map[string]any
	json.Unmarshal(out[len(out)-2], &usageBlk)
	u := usageBlk["usage"].(map[string]any)
	if u["prompt_tokens"] != float64(9) || u["completion_tokens"] != float64(2) {
		t.Errorf("尾块 usage = %v", u)
	}
	if strings.TrimSpace(string(out[len(out)-1])) != "[DONE]" {
		t.Errorf("结尾应为 [DONE], got %s", out[len(out)-1])
	}
}

func TestChatConverterFromAnthropicUpstream(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_x","model":"up-claude","usage":{"input_tokens":12}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	}
	text, out, usage := feedAll(t, model.ProtocolChatCompletions, model.ProtocolMessages, events)
	if text != "你好" {
		t.Fatalf("text = %q", text)
	}
	// input_tokens 来自 message_start，completion 来自 message_delta
	if usage.PromptTokens != 12 || usage.CompletionTokens != 3 || usage.TotalTokens != 15 {
		t.Errorf("usage = %+v", usage)
	}
	// 首块 id/model 应从嵌套 message 中提取
	var first map[string]any
	json.Unmarshal(out[0], &first)
	if first["id"] != "msg_x" || first["model"] != "up-claude" {
		t.Errorf("首块 id/model = %v/%v", first["id"], first["model"])
	}
	// 上游 max_tokens 截断：chat finish_reason 应为 length
	var finish map[string]any
	json.Unmarshal(out[len(out)-3], &finish)
	ch := finish["choices"].([]any)[0].(map[string]any)
	if ch["finish_reason"] != "length" {
		t.Errorf("finish_reason = %v, want length", ch["finish_reason"])
	}
}

func TestChatConverterFromResponsesUpstream(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_9","model":"up-gpt"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`,
		`{"type":"response.output_text.delta","delta":"hi","item_id":"msg_0","output_index":0,"content_index":0}`,
		`{"type":"response.completed","response":{"id":"resp_9","usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}}`,
	}
	text, out, usage := feedAll(t, model.ProtocolChatCompletions, model.ProtocolResponses, events)
	if text != "hi" {
		t.Fatalf("text = %q", text)
	}
	if usage.PromptTokens != 4 || usage.CompletionTokens != 1 {
		t.Errorf("usage = %+v", usage)
	}
	var first map[string]any
	json.Unmarshal(out[0], &first)
	if first["id"] != "resp_9" || first["model"] != "up-gpt" {
		t.Errorf("首块 id/model = %v/%v", first["id"], first["model"])
	}
}

func TestChatConverterNoUsageEstimates(t *testing.T) {
	// 上游不报告 usage：completion 本地估算兜底
	c := &chatChunkConverter{parser: NewStreamParser(model.ProtocolChatCompletions)}
	c.Feed([]byte(`{"id":"x","choices":[{"delta":{"content":"一二三四五六"}}]}`))
	_, usage := c.Finalize()
	if usage.Source != UsageFromEstimate || usage.CompletionTokens <= 0 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestChatConverterUpstreamUsageMissingCompletion(t *testing.T) {
	// 上游 usage 缺 output_tokens：估算补全并标记 estimate 来源
	c := &chatChunkConverter{parser: NewStreamParser(model.ProtocolChatCompletions)}
	c.Feed([]byte(`{"id":"x","choices":[{"delta":{"content":"一二三四五六"}}]}`))
	c.Feed([]byte(`{"choices":[],"usage":{"prompt_tokens":10}}`))
	_, usage := c.Finalize()
	if usage.Source != UsageFromEstimate {
		t.Errorf("含估算成分应标记 estimate, got %s", usage.Source)
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens <= 0 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestChatConverterInvalidChunkSkipped(t *testing.T) {
	c := &chatChunkConverter{parser: NewStreamParser(model.ProtocolChatCompletions)}
	if _, err := c.Feed([]byte(`{bad`)); err != nil {
		t.Fatalf("非法块应跳过: %v", err)
	}
	out, err := c.Feed([]byte(`{"id":"x","choices":[{"delta":{"content":"ok"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var first map[string]any
	json.Unmarshal(out[0], &first)
	if first["id"] != "x" {
		t.Errorf("非法块应被跳过, first = %v", first)
	}
	if c.Text() != "ok" {
		t.Errorf("text = %q", c.Text())
	}
}

// ---------- 客户端: messages ----------

func TestAnthropicConverterFromOpenAIUpstream(t *testing.T) {
	events := []string{
		`{"id":"c1","model":"up-gpt","choices":[{"delta":{"content":"He"}}]}`,
		`{"id":"c1","choices":[{"delta":{"content":"y"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":6,"completion_tokens":2}}`,
	}
	text, out, usage := feedAll(t, model.ProtocolMessages, model.ProtocolChatCompletions, events)
	if text != "Hey" {
		t.Fatalf("text = %q", text)
	}
	if usage.PromptTokens != 6 || usage.CompletionTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
	types := make([]string, 0)
	for _, e := range out {
		var m map[string]any
		json.Unmarshal(e, &m)
		if v, ok := m["type"].(string); ok {
			types = append(types, v)
		}
	}
	// 事件生命周期与官方一致（openai 上游分两个 delta）
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("事件序列 = %v", types)
	}
	// message_start 的 message 对象官方为严格校验：id/model/usage 必填。
	// openai 上游首块无 usage 时按零值补全（省略字段会导致 SDK 解析失败）。
	var start map[string]any
	json.Unmarshal(out[0], &start)
	msg := start["message"].(map[string]any)
	if msg["id"] != "c1" || msg["model"] == nil || msg["model"] == "" {
		t.Errorf("message_start 缺 id/model: %v", msg)
	}
	su, ok := msg["usage"].(map[string]any)
	if !ok || su["input_tokens"] != float64(0) || su["output_tokens"] != float64(0) {
		t.Errorf("message_start.usage 应零值补全: %v", msg["usage"])
	}
	// message_delta 应含 output_tokens 与 stop_reason
	var delta map[string]any
	json.Unmarshal(out[len(out)-2], &delta)
	d := delta["delta"].(map[string]any)
	if d["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v", d["stop_reason"])
	}
	if delta["usage"].(map[string]any)["output_tokens"] != float64(2) {
		t.Errorf("output_tokens = %v", delta["usage"])
	}
}

func TestAnthropicConverterStopReasonMapping(t *testing.T) {
	// 上游 chat finish_reason=length → anthropic max_tokens
	events := []string{
		`{"choices":[{"delta":{"content":"x"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
	}
	_, out, _ := feedAll(t, model.ProtocolMessages, model.ProtocolChatCompletions, events)
	var delta map[string]any
	json.Unmarshal(out[len(out)-2], &delta)
	if got := delta["delta"].(map[string]any)["stop_reason"]; got != "max_tokens" {
		t.Errorf("stop_reason = %v, want max_tokens", got)
	}
}

func TestAnthropicConverterFromResponsesUpstream(t *testing.T) {
	events := []string{
		`{"type":"response.output_text.delta","delta":"abc"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1}}}`,
	}
	text, out, usage := feedAll(t, model.ProtocolMessages, model.ProtocolResponses, events)
	if text != "abc" {
		t.Fatalf("text = %q", text)
	}
	if usage.PromptTokens != 1 || usage.CompletionTokens != 1 {
		t.Errorf("usage = %+v", usage)
	}
	if len(out) < 6 {
		t.Fatalf("事件数 = %d", len(out))
	}
}

// ---------- 客户端: responses ----------

func TestResponsesConverterFromOpenAIUpstream(t *testing.T) {
	events := []string{
		`{"id":"c1","model":"up-gpt","choices":[{"delta":{"content":"He"}}]}`,
		`{"id":"c1","choices":[{"delta":{"content":"y"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":6,"completion_tokens":2,"total_tokens":8}}`,
	}
	text, out, usage := feedAll(t, model.ProtocolResponses, model.ProtocolChatCompletions, events)
	if text != "Hey" {
		t.Fatalf("text = %q", text)
	}
	if usage.PromptTokens != 6 || usage.CompletionTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
	types := make([]string, 0)
	var seqLast float64
	for _, e := range out {
		var m map[string]any
		json.Unmarshal(e, &m)
		types = append(types, m["type"].(string))
		if v, ok := m["sequence_number"].(float64); ok && v > seqLast {
			seqLast = v
		}
	}
	// 事件顺序：created → in_progress → output_item.added → content_part.added → delta* → done 系列
	want := []string{
		"response.created", "response.in_progress", "response.output_item.added",
		"response.content_part.added", "response.output_text.delta", "response.output_text.delta",
		"response.output_text.done", "response.content_part.done",
		"response.output_item.done", "response.completed",
	}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("事件序列 = %v", types)
	}
	// sequence_number 单调递增且无空洞
	if seqLast != float64(len(types)-1) {
		t.Errorf("sequence_number 尾值 = %v, want %d", seqLast, len(types)-1)
	}
	// delta 事件必须带 item_id（客户端依赖归属）
	for i, ty := range types {
		if ty == "response.output_text.delta" {
			var m map[string]any
			json.Unmarshal(out[i], &m)
			if m["item_id"] != "msg_0" || m["output_index"] != float64(0) || m["content_index"] != float64(0) {
				t.Errorf("delta 归属字段 = %v", m)
			}
			break
		}
	}
	// response.completed 携带 usage 与最终 output
	var completed map[string]any
	json.Unmarshal(out[len(out)-1], &completed)
	resp := completed["response"].(map[string]any)
	if resp["status"] != "completed" || resp["model"] != "up-gpt" {
		t.Errorf("response = %v", resp)
	}
	u := resp["usage"].(map[string]any)
	if u["input_tokens"] != float64(6) || u["output_tokens"] != float64(2) || u["total_tokens"] != float64(8) {
		t.Errorf("usage = %v", u)
	}
	items := resp["output"].([]any)
	item := items[0].(map[string]any)
	if item["status"] != "completed" {
		t.Errorf("item = %v", item)
	}
	part := item["content"].([]any)[0].(map[string]any)
	if part["text"] != "Hey" {
		t.Errorf("最终文本 = %v", part["text"])
	}
}

func TestResponsesConverterEmptyOutput(t *testing.T) {
	// 无任何文本输出：不产生 content_part/delta 事件，但仍收尾完整生命周期
	events := []string{`{"id":"c1","choices":[{"delta":{"content":""}}]}`}
	_, out, _ := feedAll(t, model.ProtocolResponses, model.ProtocolChatCompletions, events)
	types := make([]string, 0)
	for _, e := range out {
		var m map[string]any
		json.Unmarshal(e, &m)
		types = append(types, m["type"].(string))
	}
	joined := strings.Join(types, ",")
	if strings.Contains(joined, "content_part") || strings.Contains(joined, "output_text") {
		t.Errorf("空输出不应产生 text 事件: %v", types)
	}
	if !strings.HasSuffix(joined, "response.completed") {
		t.Errorf("应以 response.completed 收尾: %v", types)
	}
}

func TestResponsesConverterFromAnthropicUpstream(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"up-claude","usage":{"input_tokens":5}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"好"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
	}
	text, out, usage := feedAll(t, model.ProtocolResponses, model.ProtocolMessages, events)
	if text != "好" {
		t.Fatalf("text = %q", text)
	}
	if usage.PromptTokens != 5 || usage.CompletionTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
	// 上游 message_start 自带 usage：usage 应汇入 response.completed；model 应从嵌套提取
	var created map[string]any
	json.Unmarshal(out[0], &created)
	resp := created["response"].(map[string]any)
	if resp["model"] != "up-claude" {
		t.Errorf("response.created.model = %v, want up-claude", resp["model"])
	}
	// input_tokens 来自 message_start，应进入最终 usage
	if usage.PromptTokens != 5 {
		t.Errorf("最终 usage.input_tokens = %d, want 5", usage.PromptTokens)
	}
}

// ---------- StreamParser 直测 ----------

func TestNewStreamParser(t *testing.T) {
	if _, ok := NewStreamParser(model.ProtocolMessages).(*anthropicStreamParser); !ok {
		t.Error("messages 上游应返回 anthropicStreamParser")
	}
	for _, p := range []model.Protocol{model.ProtocolChatCompletions, model.ProtocolResponses} {
		if _, ok := NewStreamParser(p).(*openaiStreamParser); !ok {
			t.Errorf("%s 上游应返回 openaiStreamParser", p)
		}
	}
}

func TestOpenAIStreamParserResponsesLifecycle(t *testing.T) {
	p := NewStreamParser(model.ProtocolResponses)
	for _, ev := range []string{
		`{"type":"response.output_text.delta","delta":"A"}`,
		`{"type":"response.output_text.delta","delta":"B"}`,
		`{"type":"response.output_text.done","text":"AB"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":2}}}`,
	} {
		if err := p.Feed([]byte(ev)); err != nil {
			t.Fatal(err)
		}
	}
	if p.Text() != "AB" {
		t.Errorf("text = %q", p.Text())
	}
	u, ok := p.Usage()
	if !ok || u.PromptTokens != 3 || u.CompletionTokens != 2 {
		t.Errorf("usage = %+v ok = %v", u, ok)
	}
}

func TestAnthropicStreamParserCacheFields(t *testing.T) {
	p := NewStreamParser(model.ProtocolMessages)
	for _, ev := range []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"text"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	} {
		if err := p.Feed([]byte(ev)); err != nil {
			t.Fatal(err)
		}
	}
	u, ok := p.Usage()
	if !ok {
		t.Fatal("应报告 usage")
	}
	if u.PromptTokens != 50 || u.CachedTokens != 10 || u.CacheWriteTokens != 5 ||
		u.CompletionTokens != 7 || u.TotalTokens != 57 {
		t.Errorf("usage = %+v", u)
	}
	if u.Source != UsageFromUpstream {
		t.Errorf("source = %s", u.Source)
	}
	if p.Text() != "text" {
		t.Errorf("input_json_delta 参数不应计入 Text（应走 ArgsText）, text = %q", p.Text())
	}
	if p.FinishReason() != "end_turn" {
		t.Errorf("finish = %q", p.FinishReason())
	}
}

func TestAnthropicStreamParserNoUsage(t *testing.T) {
	p := NewStreamParser(model.ProtocolMessages)
	if _, ok := p.Usage(); ok {
		t.Error("未收到 usage 事件时应返回 false")
	}
	// message_start 无 usage 字段
	p.Feed([]byte(`{"type":"message_start","message":{}}`))
	if _, ok := p.Usage(); ok {
		t.Error("message_start 无 usage 时不应标记")
	}
	// message_delta 带 usage
	p.Feed([]byte(`{"type":"message_delta","delta":{},"usage":{"output_tokens":4}}`))
	u, ok := p.Usage()
	if !ok || u.CompletionTokens != 4 || u.TotalTokens != 4 {
		t.Errorf("usage = %+v ok = %v", u, ok)
	}
	if p.FinishReason() != "" {
		t.Errorf("无 stop_reason 时 finish 应为空")
	}
}

func TestOpenAIStreamParserFinishReason(t *testing.T) {
	p := NewStreamParser(model.ProtocolChatCompletions)
	p.Feed([]byte(`{"choices":[{"delta":{"content":"x"}}]}`))
	if p.FinishReason() != "" {
		t.Errorf("初始 finish 应为空")
	}
	p.Feed([]byte(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`))
	if p.FinishReason() != "stop" {
		t.Errorf("finish = %q", p.FinishReason())
	}
}

func TestOpenAIStreamParserInvalidAndEmpty(t *testing.T) {
	p := NewStreamParser(model.ProtocolChatCompletions)
	if err := p.Feed([]byte(`{bad`)); err != nil {
		t.Errorf("非法块应静默跳过, got %v", err)
	}
	if err := p.Feed([]byte(`"plain string"`)); err != nil {
		t.Errorf("非对象块应跳过, got %v", err)
	}
	if p.Text() != "" {
		t.Errorf("text = %q", p.Text())
	}
}

// ---------- estimate 相关 ----------

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Error("空文本应为 0")
	}
	if EstimateTokens("a") < 1 {
		t.Error("短文本至少 1 token")
	}
	en := EstimateTokens(strings.Repeat("abcd", 100)) // 400 拉丁字符 ≈ 100
	if en < 90 || en > 110 {
		t.Errorf("拉丁估算 = %d", en)
	}
	zh := EstimateTokens(strings.Repeat("好", 150)) // 150 CJK ≈ 100
	if zh < 90 || zh > 110 {
		t.Errorf("CJK 估算 = %d", zh)
	}
}

func TestEstimateBytes(t *testing.T) {
	if EstimateBytes([]byte("hello world")) <= 0 {
		t.Error("合法 UTF-8 应正常估算")
	}
	// 非法 UTF-8：len/4 兜底
	if got := EstimateBytes([]byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xfb, 0xfc}); got != 2 {
		t.Errorf("非法 UTF-8 估算 = %d", got)
	}
}

// ---------- 端到端双向流式一致性 ----------

func TestStreamRoundTripTextIntegrity(t *testing.T) {
	// anthropic 客户端 ↔ openai 上游：文本分片重组后应完全一致
	upEvents := []string{
		`{"id":"c","choices":[{"delta":{"content":"Hello, "}}]}`,
		`{"id":"c","choices":[{"delta":{"content":"世界!"}}]}`,
	}
	for _, in := range model.AllProtocols {
		if in == model.ProtocolChatCompletions {
			continue
		}
		text, _, _ := feedAll(t, in, model.ProtocolChatCompletions, upEvents)
		if text != "Hello, 世界!" {
			t.Errorf("%s 客户端重组文本 = %q", in, text)
		}
	}
}

// ---------- mock parser：覆盖防御分支 ----------

type mockStreamParser struct {
	err    error
	text   string
	usage  Usage
	has    bool
	finish string
}

func (m *mockStreamParser) Feed([]byte) error    { return m.err }
func (m *mockStreamParser) Usage() (Usage, bool) { return m.usage, m.has }
func (m *mockStreamParser) Text() string         { return m.text }
func (m *mockStreamParser) ArgsText() string     { return "" }
func (m *mockStreamParser) FinishReason() string { return m.finish }

func TestStreamConverterParserErrorPropagates(t *testing.T) {
	// 解析器出错时三类转换器都应中断 Feed
	m := &mockStreamParser{err: errInvalidBaseURL("boom")}
	if _, err := (&chatChunkConverter{parser: m}).Feed([]byte(`{}`)); err == nil {
		t.Error("chat 转换器应透传解析错误")
	}
	if _, err := (&anthropicEventConverter{streamConvBase: streamConvBase{parser: m}}).Feed([]byte(`{}`)); err == nil {
		t.Error("anthropic 转换器应透传解析错误")
	}
	if _, err := (&responsesEventConverter{streamConvBase: streamConvBase{parser: m}}).Feed([]byte(`{}`)); err == nil {
		t.Error("responses 转换器应透传解析错误")
	}
}

func TestStreamFinalizeFlushesPendingText(t *testing.T) {
	// 文本在 Feed 之后增长（如解析器缓冲）：Finalize 应冲刷残余文本
	m := &mockStreamParser{text: "tail"}
	c := &chatChunkConverter{parser: m}
	out, _ := c.Finalize()
	var first map[string]any
	json.Unmarshal(out[0], &first)
	if ch := first["choices"].([]any)[0].(map[string]any); ch["delta"].(map[string]any)["content"] != "tail" {
		t.Errorf("chat Finalize 应冲刷 pending: %v", ch)
	}

	a := &anthropicEventConverter{streamConvBase: streamConvBase{parser: &mockStreamParser{text: "tail"}}}
	outA, _ := a.Finalize()
	var ev map[string]any
	json.Unmarshal(outA[0], &ev)
	if ev["type"] != "content_block_delta" || ev["delta"].(map[string]any)["text"] != "tail" {
		t.Errorf("anthropic Finalize 应冲刷 pending: %v", ev)
	}

	r := &responsesEventConverter{streamConvBase: streamConvBase{parser: &mockStreamParser{text: "tail"}}}
	outR, _ := r.Finalize()
	// 惰性 message 条目：out[0] 为条目宣告，随后的 output_text.delta 冲刷残余文本
	var evR map[string]any
	json.Unmarshal(outR[0], &evR)
	if evR["type"] != "response.output_item.added" {
		t.Errorf("responses Finalize 应先宣告 message 条目: %v", evR)
	}
	json.Unmarshal(outR[1], &evR)
	if evR["type"] != "response.output_text.delta" || evR["delta"] != "tail" {
		t.Errorf("responses Finalize 应冲刷 pending: %v", evR)
	}
}

func TestResponsesConverterHeadFromResponsesEvents(t *testing.T) {
	// responses 事件流的 id/model 位于 response.* 嵌套：应提取进 response.created
	r := &responsesEventConverter{streamConvBase: streamConvBase{parser: NewStreamParser(model.ProtocolResponses)}}
	out, err := r.Feed([]byte(`{"type":"response.created","response":{"id":"resp_real","model":"up-gpt"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(out[0], &m)
	resp := m["response"].(map[string]any)
	if resp["id"] != "resp_real" || resp["model"] != "up-gpt" {
		t.Errorf("response.created = %v", resp)
	}
}

func TestAnthropicMessageStartCarriesUpstreamUsage(t *testing.T) {
	// 上游首块自带 usage：message_start 应携带 input_tokens
	c := &anthropicEventConverter{streamConvBase: streamConvBase{parser: NewStreamParser(model.ProtocolChatCompletions)}}
	out, err := c.Feed([]byte(`{"choices":[],"usage":{"prompt_tokens":8,"prompt_tokens_details":{"cached_tokens":3}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(out[0], &m)
	u := m["message"].(map[string]any)["usage"].(map[string]any)
	if u["input_tokens"] != float64(8) || u["cache_read_input_tokens"] != float64(3) {
		t.Errorf("message_start.usage = %v", u)
	}
}

func TestResponsesConverterLengthIncomplete(t *testing.T) {
	// 上游 length 截断：官方形态是 response.incomplete + incomplete_details，
	// 而非 response.completed（客户端据此区分截断与正常结束）。
	events := []string{
		`{"id":"c1","model":"m","choices":[{"delta":{"content":"trunc"},"finish_reason":null}]}`,
		`{"id":"c1","model":"m","choices":[{"delta":{},"finish_reason":"length"}]}`,
	}
	_, out, _ := feedAll(t, model.ProtocolResponses, model.ProtocolChatCompletions, events)
	var ev map[string]any
	if err := json.Unmarshal(out[len(out)-1], &ev); err != nil {
		t.Fatalf("末事件非法: %v", err)
	}
	if ev["type"] != "response.incomplete" {
		t.Fatalf("末事件 = %v, want response.incomplete", ev["type"])
	}
	resp := ev["response"].(map[string]any)
	if resp["status"] != "incomplete" {
		t.Errorf("response.status = %v, want incomplete", resp["status"])
	}
	det, _ := resp["incomplete_details"].(map[string]any)
	if det["reason"] != "max_output_tokens" {
		t.Errorf("incomplete_details = %v", resp["incomplete_details"])
	}
	items := resp["output"].([]any)
	if item := items[0].(map[string]any); item["status"] != "incomplete" {
		t.Errorf("item.status = %v, want incomplete", item["status"])
	}
}

// ---------- 流式工具调用转换 ----------

func joinFeed(t *testing.T, c StreamConverter, events []string) string {
	t.Helper()
	var sb strings.Builder
	for _, e := range events {
		chunks, err := c.Feed([]byte(e))
		if err != nil {
			t.Fatal(err)
		}
		for _, chunk := range chunks {
			sb.Write(chunk)
			sb.WriteString("\n")
		}
	}
	done, _ := c.Finalize()
	for _, chunk := range done {
		sb.Write(chunk)
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestStreamToolsAnthropicToChat(t *testing.T) {
	// anthropic 上游 tool_use 流 -> chat 客户端 delta.tool_calls
	c, err := NewStreamConverter(model.ProtocolChatCompletions, model.ProtocolMessages)
	if err != nil {
		t.Fatal(err)
	}
	out := joinFeed(t, c, []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude","usage":{"input_tokens":9}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"查一下"}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"北京\"}"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
	})
	if !strings.Contains(out, `"tool_calls"`) {
		t.Fatalf("应产出 delta.tool_calls: %s", out)
	}
	if !strings.Contains(out, `"name":"get_weather"`) {
		t.Errorf("工具名缺失: %s", out)
	}
	if !strings.Contains(out, `"{\"city\":`) || !strings.Contains(out, `\"北京\"}`) {
		t.Errorf("参数增量不完整: %s", out)
	}
	if strings.Contains(out, `"content":"{\"city\":`) {
		t.Errorf("工具参数不应泄漏为文本 content: %s", out)
	}
	if !strings.Contains(out, `"finish_reason":"tool_calls"`) {
		t.Errorf("finish_reason 应为 tool_calls: %s", out)
	}
	// 官方 SDK 严格校验
	var chunks []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			chunks = append(chunks, m)
		}
	}
	for _, m := range chunks {
		for _, k := range []string{"id", "object", "created", "model", "choices"} {
			if _, ok := m[k]; !ok {
				t.Errorf("chunk 缺必填 %s: %v", k, m)
			}
		}
	}
}

func TestStreamToolsChatToResponses(t *testing.T) {
	// chat 上游 tool_calls 流 -> responses 客户端 function_call 事件
	c, err := NewStreamConverter(model.ProtocolResponses, model.ProtocolChatCompletions)
	if err != nil {
		t.Fatal(err)
	}
	out := joinFeed(t, c, []string{
		`{"id":"chatcmpl-1","model":"m","choices":[{"delta":{"content":"查一下"}}]}`,
		`{"id":"chatcmpl-1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"id":"chatcmpl-1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}}]}`,
		`{"id":"chatcmpl-1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"北京\"}"}}]}}]}`,
		`{"id":"chatcmpl-1","model":"m","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"chatcmpl-1","model":"m","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14}}`,
	})
	for _, want := range []string{
		`"response.created"`,
		`"response.output_item.added"`,
		`"type":"function_call"`,
		`"response.function_call_arguments.delta"`,
		`"response.function_call_arguments.done"`,
		`"response.output_item.done"`,
		`"response.completed"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺事件 %s: %s", want, out)
		}
	}
	// 参数完整汇聚到 done 与 output 条目
	if !strings.Contains(out, `"{\"city\":\"北京\"}"`) {
		t.Errorf("参数未完整汇聚: %s", out)
	}
	// 有文本：message 条目也应存在
	if !strings.Contains(out, `"msg_0"`) {
		t.Errorf("有文本时应含 message 条目: %s", out)
	}
}

func TestStreamToolsChatToAnthropic(t *testing.T) {
	// chat 上游 tool_calls 流 -> anthropic 客户端 tool_use 块
	c, err := NewStreamConverter(model.ProtocolMessages, model.ProtocolChatCompletions)
	if err != nil {
		t.Fatal(err)
	}
	out := joinFeed(t, c, []string{
		`{"id":"chatcmpl-1","model":"m","choices":[{"delta":{"content":"hi"}}]}`,
		`{"id":"chatcmpl-1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"f","arguments":"{}"}}]}}]}`,
		`{"id":"chatcmpl-1","model":"m","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	})
	var sawToolStart, sawTextStop, sawJSONDelta, sawToolStop bool
	var stopReason string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch ev["type"] {
		case "content_block_start":
			if blk, _ := ev["content_block"].(map[string]any); blk != nil &&
				blk["type"] == "tool_use" && blk["id"] == "call_1" && blk["name"] == "f" {
				sawToolStart = true
			}
		case "content_block_stop":
			if idx, _ := ev["index"].(float64); idx == 0 {
				sawTextStop = true // 文本块先闭合
			}
			if idx, _ := ev["index"].(float64); idx == 1 {
				sawToolStop = true
			}
		case "content_block_delta":
			if d, _ := ev["delta"].(map[string]any); d != nil && d["type"] == "input_json_delta" {
				sawJSONDelta = true
			}
		case "message_delta":
			if d, _ := ev["delta"].(map[string]any); d != nil {
				stopReason, _ = d["stop_reason"].(string)
			}
		}
	}
	if !sawTextStop || !sawToolStart || !sawJSONDelta || !sawToolStop {
		t.Errorf("tool_use 块生命周期不完整: %s", out)
	}
	if stopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", stopReason)
	}
}

func TestStreamToolsResponsesToChatPureToolCall(t *testing.T) {
	// responses 上游纯 function_call（无文本）-> chat 客户端：不产生空 content 消息
	c, err := NewStreamConverter(model.ProtocolChatCompletions, model.ProtocolResponses)
	if err != nil {
		t.Fatal(err)
	}
	out := joinFeed(t, c, []string{
		`{"type":"response.created","response":{"id":"resp_1","model":"m"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"f","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":3,"output_tokens":1}}}`,
	})
	if !strings.Contains(out, `"tool_calls"`) || !strings.Contains(out, `"name":"f"`) {
		t.Errorf("应产出工具调用: %s", out)
	}
	if !strings.Contains(out, `"finish_reason":"tool_calls"`) {
		t.Errorf("finish_reason 应为 tool_calls: %s", out)
	}
	if strings.Contains(out, `"content":"`) {
		t.Errorf("纯工具调用不应产生文本 content: %s", out)
	}
}
