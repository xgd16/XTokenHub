package provider_test

import (
	"xtokenhub/internal/provider"

	"encoding/json"
	"strings"
	"testing"

	"xtokenhub/internal/model"
)

func feedAll(t *testing.T, conv provider.StreamConverter, items []string) ([][]byte, error) {
	t.Helper()
	var out [][]byte
	for _, d := range items {
		chunks, err := conv.Feed([]byte(d))
		if err != nil {
			return nil, err
		}
		out = append(out, chunks...)
	}
	return out, nil
}

func TestStreamConvertAnthropicToChat(t *testing.T) {
	conv, err := provider.NewStreamConverter(model.ProtocolChatCompletions, model.ProtocolMessages)
	if err != nil {
		t.Fatal(err)
	}
	// 上游 anthropic SSE 事件
	upEvents := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":50}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"héllo "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"世界"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`,
		`{"type":"message_stop"}`,
	}
	out, err := feedAll(t, conv, upEvents)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, c := range out {
		var chunk map[string]any
		if err := json.Unmarshal(c, &chunk); err != nil {
			t.Fatalf("非法客户端 chunk: %s", c)
		}
		if chunk["object"] == "chat.completion.chunk" {
			choices := chunk["choices"].([]any)
			if len(choices) > 0 {
				delta := choices[0].(map[string]any)["delta"].(map[string]any)
				if s, ok := delta["content"].(string); ok {
					text.WriteString(s)
				}
			}
		}
	}
	if text.String() != "héllo 世界" {
		t.Errorf("增量文本错误: %q", text.String())
	}

	done, usage := conv.Finalize()
	// [DONE] 结尾
	last := done[len(done)-1]
	if string(last) != "[DONE]" {
		t.Errorf("结尾应为 [DONE]: %s", last)
	}
	// usage 块
	found := false
	for _, c := range done {
		var chunk map[string]any
		if json.Unmarshal(c, &chunk) == nil {
			if u, ok := chunk["usage"].(map[string]any); ok {
				found = true
				if u["prompt_tokens"] != float64(50) || u["completion_tokens"] != float64(9) {
					t.Errorf("usage 块错误: %v", u)
				}
			}
		}
	}
	if !found {
		t.Error("缺少 usage 块")
	}
	if usage.PromptTokens != 50 || usage.CompletionTokens != 9 {
		t.Errorf("返回 usage: %+v", usage)
	}
	if usage.Source != provider.UsageFromUpstream {
		t.Errorf("source = %s", usage.Source)
	}
}

func TestStreamConvertOpenAIToAnthropic(t *testing.T) {
	conv, err := provider.NewStreamConverter(model.ProtocolMessages, model.ProtocolChatCompletions)
	if err != nil {
		t.Fatal(err)
	}
	upChunks := []string{
		`{"id":"cc1","model":"gpt-x","choices":[{"delta":{"role":"assistant","content":"A"}}]}`,
		`{"id":"cc1","model":"gpt-x","choices":[{"delta":{"content":"B"}}]}`,
		`{"id":"cc1","model":"gpt-x","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"cc1","usage":{"prompt_tokens":11,"completion_tokens":2}}`,
	}
	out, err := feedAll(t, conv, upChunks)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]int{}
	var text strings.Builder
	for _, c := range out {
		var ev map[string]any
		if err := json.Unmarshal(c, &ev); err != nil {
			t.Fatalf("非法 anthropic 事件: %s", c)
		}
		typ, _ := ev["type"].(string)
		types[typ]++
		if typ == "content_block_delta" {
			text.WriteString(ev["delta"].(map[string]any)["text"].(string))
		}
	}
	if types["message_start"] == 0 || types["content_block_start"] == 0 {
		t.Errorf("缺少起始事件: %v", types)
	}
	if text.String() != "AB" {
		t.Errorf("text = %q", text.String())
	}
	done, usage := conv.Finalize()
	types = map[string]int{}
	for _, c := range done {
		var ev map[string]any
		_ = json.Unmarshal(c, &ev)
		types[ev["type"].(string)]++
	}
	if types["message_delta"] == 0 || types["message_stop"] == 0 {
		t.Errorf("缺少收尾事件: %v", types)
	}
	if usage.PromptTokens != 11 || usage.CompletionTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestStreamConvertToResponses(t *testing.T) {
	for _, up := range []model.Protocol{model.ProtocolChatCompletions, model.ProtocolMessages} {
		conv, err := provider.NewStreamConverter(model.ProtocolResponses, up)
		if err != nil {
			t.Fatal(err)
		}
		var upEvents []string
		if up == model.ProtocolChatCompletions {
			upEvents = []string{
				`{"id":"c1","model":"m","choices":[{"delta":{"content":"yo"}}]}`,
				`{"id":"c1","usage":{"prompt_tokens":5,"completion_tokens":1}}`,
			}
		} else {
			upEvents = []string{
				`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
				`{"type":"content_block_delta","delta":{"type":"text_delta","text":"yo"}}`,
				`{"type":"message_delta","usage":{"output_tokens":1}}`,
			}
		}
		out, err := feedAll(t, conv, upEvents)
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		// 事件生命周期：delta 之前必须先宣告 item 与 content part（缺失时
		// 客户端报 "text part msg_0 not found"），收尾必须有 done/completed
		var order []string
		sawCreated, sawItemAdded, sawPartAdded := false, false, false
		for _, c := range out {
			var ev map[string]any
			if err := json.Unmarshal(c, &ev); err != nil {
				t.Fatalf("非法 responses 事件: %s", c)
			}
			typ, _ := ev["type"].(string)
			order = append(order, typ)
			switch typ {
			case "response.created":
				sawCreated = true
			case "response.output_item.added":
				item := ev["item"].(map[string]any)
				if item["id"] != "msg_0" || item["type"] != "message" {
					t.Errorf("output_item.added item: %v", item)
				}
				sawItemAdded = true
			case "response.content_part.added":
				part := ev["part"].(map[string]any)
				if part["type"] != "output_text" || ev["item_id"] != "msg_0" {
					t.Errorf("content_part.added: %v", ev)
				}
				sawPartAdded = true
			case "response.output_text.delta":
				if !sawItemAdded || !sawPartAdded {
					t.Errorf("delta 先于 item/part 宣告: %v", order)
				}
				text.WriteString(ev["delta"].(string))
			}
		}
		if !sawCreated || !sawItemAdded || !sawPartAdded {
			t.Errorf("事件缺失 created/item.added/part.added: %v", order)
		}
		if text.String() != "yo" {
			t.Errorf("text = %q (up=%s)", text.String(), up)
		}
		done, usage := conv.Finalize()
		var completed map[string]any
		doneTypes := map[string]bool{}
		for _, c := range done {
			var ev map[string]any
			if json.Unmarshal(c, &ev) == nil {
				if typ, _ := ev["type"].(string); typ != "" {
					doneTypes[typ] = true
				}
				if ev["type"] == "response.completed" {
					completed = ev
				}
			}
		}
		for _, want := range []string{"response.output_text.done", "response.content_part.done", "response.output_item.done", "response.completed"} {
			if !doneTypes[want] {
				t.Errorf("收尾缺少 %s (up=%s): %v", want, up, doneTypes)
			}
		}
		if completed == nil {
			t.Fatalf("缺少 response.completed (up=%s)", up)
		}
		resp := completed["response"].(map[string]any)
		if resp["id"] != "resp_0" || resp["status"] != "completed" {
			t.Errorf("completed response 头部: %v", resp)
		}
		output := resp["output"].([]any)
		item := output[0].(map[string]any)
		if item["id"] != "msg_0" || item["status"] != "completed" {
			t.Errorf("completed output item: %v", item)
		}
		u := resp["usage"].(map[string]any)
		if u["input_tokens"] != float64(5) || u["output_tokens"] != float64(1) {
			t.Errorf("completed usage: %v", u)
		}
		if usage.TotalTokens != 6 {
			t.Errorf("usage = %+v", usage)
		}
	}
}

func TestStreamConvertFallbackEstimate(t *testing.T) {
	// 相同协议应拒绝构造转换器
	if _, err := provider.NewStreamConverter(model.ProtocolChatCompletions, model.ProtocolChatCompletions); err == nil {
		t.Fatal("相同协议应拒绝构造转换器")
	}
	conv2, err := provider.NewStreamConverter(model.ProtocolResponses, model.ProtocolMessages)
	if err != nil {
		t.Fatal(err)
	}
	_, err = feedAll(t, conv2, []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":3}}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"some words here"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, usage := conv2.Finalize()
	if usage.Source != provider.UsageFromEstimate {
		t.Errorf("应走估算兜底: %+v", usage)
	}
	if usage.PromptTokens != 3 || usage.CompletionTokens <= 0 {
		t.Errorf("usage = %+v", usage)
	}
}
