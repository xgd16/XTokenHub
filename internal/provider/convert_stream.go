package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"xtokenhub/internal/model"
)

// StreamConverter 流式协议转换器（转换路径专用）：
// 喂入上游 SSE data 载荷，产出客户端协议的 SSE data 载荷。
// 文本增量统一经中间解析器提取，输出按客户端协议格式重组。
type StreamConverter interface {
	// Feed 输入一条上游 SSE data 载荷，返回应发给客户端的 data 载荷切片。
	Feed(data []byte) ([][]byte, error)
	// Finalize 流结束时调用，返回收尾块与最终 usage。
	Finalize() ([][]byte, Usage)
	// Text 已累积的输出文本（供实时吞吐估算用；未转换原样透传时可能为 0）。
	Text() string
}

// NewStreamConverter 构造转换器；in 为客户端协议，up 为上游协议。
// 原生透传不应调用此构造器（透传不转换）。
func NewStreamConverter(in, up model.Protocol) (StreamConverter, error) {
	if in == up {
		return nil, fmt.Errorf("原生协议无需转换（%s）", in)
	}
	switch in {
	case model.ProtocolChatCompletions:
		return &chatChunkConverter{parser: NewStreamParser(up)}, nil
	case model.ProtocolMessages:
		return &anthropicEventConverter{parser: NewStreamParser(up)}, nil
	case model.ProtocolResponses:
		return &responsesEventConverter{parser: NewStreamParser(up)}, nil
	default:
		return nil, errUnsupportedProtocol(in)
	}
}

// streamConvBase 公共骨架：解析上游 -> 增量文本 -> 客户端事件。
type streamConvBase struct {
	parser  StreamUsageParser
	emitted int64 // 已发出的文本长度
	started bool
}

// pending 增量文本。
func (b *streamConvBase) pending() string {
	full := b.parser.Text()
	if int64(len(full)) <= b.emitted {
		return ""
	}
	suffix := full[b.emitted:]
	b.emitted = int64(len(full))
	return suffix
}

// Text 已累积的输出文本。
func (b *streamConvBase) Text() string { return b.parser.Text() }

func (b *streamConvBase) finalizeUsage() Usage {
	u, ok := b.parser.Usage()
	est := EstimateTokens(b.parser.Text())
	if ok {
		// 上游已报告：缺失的 completion 用估算补全（部分上游不发 output_tokens）
		if u.CompletionTokens == 0 && est > 0 {
			u.CompletionTokens = est
			u.Source = UsageFromEstimate // 含估算成分，标记兜底来源
		}
		return u.Normalize()
	}
	// 上游未报告 usage：本地估算兜底
	return Usage{Source: UsageFromEstimate, CompletionTokens: est}.Normalize()
}

// ---------- 客户端: chat/completions ----------

type chatChunkConverter struct {
	streamConvBase
	id    string
	model string
}

func (c *chatChunkConverter) chunk(delta string, finish *string, usage *map[string]any) []byte {
	m := map[string]any{
		"id": c.id, "object": "chat.completion.chunk", "model": c.model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}}},
	}
	if delta != "" {
		m["choices"].([]map[string]any)[0]["delta"] = map[string]any{"content": delta}
	}
	if finish != nil {
		m["choices"].([]map[string]any)[0]["delta"] = map[string]any{}
		m["choices"].([]map[string]any)[0]["finish_reason"] = *finish
	}
	if usage != nil {
		m["usage"] = *usage
	}
	data, _ := json.Marshal(m)
	return data
}

func (c *chatChunkConverter) Feed(data []byte) ([][]byte, error) {
	if err := c.parser.Feed(data); err != nil {
		return nil, err
	}
	if !c.started {
		c.started = true
		// 首块：解析出 id/model
		var head struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		}
		_ = json.Unmarshal(data, &head)
		if head.ID != "" {
			c.id = head.ID
		}
		if head.Model != "" {
			c.model = head.Model
		}
	}
	suffix := c.pending()
	if suffix == "" {
		return nil, nil
	}
	return [][]byte{c.chunk(suffix, nil, nil)}, nil
}

func (c *chatChunkConverter) Finalize() ([][]byte, Usage) {
	out := make([][]byte, 0, 3)
	if suffix := c.pending(); suffix != "" {
		out = append(out, c.chunk(suffix, nil, nil))
	}
	u := c.finalizeUsage()
	finish := "stop"
	out = append(out, c.chunk("", &finish, nil))
	usage := map[string]any{
		"prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "total_tokens": u.TotalTokens,
		"prompt_tokens_details": map[string]any{"cached_tokens": u.CachedTokens},
	}
	out = append(out, c.chunk("", nil, &usage))
	out = append(out, []byte("[DONE]"))
	return out, u
}

// ---------- 客户端: messages ----------

type anthropicEventConverter struct {
	streamConvBase
}

func (a *anthropicEventConverter) event(typ string, fields map[string]any) []byte {
	m := map[string]any{"type": typ}
	for k, v := range fields {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	return data
}

func (a *anthropicEventConverter) Feed(data []byte) ([][]byte, error) {
	if err := a.parser.Feed(data); err != nil {
		return nil, err
	}
	var out [][]byte
	if !a.started {
		a.started = true
		out = append(out, a.event("message_start", map[string]any{
			"message": map[string]any{"type": "message", "role": "assistant", "content": []any{}},
		}))
		out = append(out, a.event("content_block_start", map[string]any{
			"index": 0, "content_block": map[string]any{"type": "text", "text": ""},
		}))
	}
	if suffix := a.pending(); suffix != "" {
		out = append(out, a.event("content_block_delta", map[string]any{
			"index": 0, "delta": map[string]any{"type": "text_delta", "text": suffix},
		}))
	}
	return out, nil
}

func (a *anthropicEventConverter) Finalize() ([][]byte, Usage) {
	out := make([][]byte, 0, 3)
	if suffix := a.pending(); suffix != "" {
		out = append(out, a.event("content_block_delta", map[string]any{
			"index": 0, "delta": map[string]any{"type": "text_delta", "text": suffix},
		}))
	}
	u := a.finalizeUsage()
	out = append(out, a.event("content_block_stop", map[string]any{"index": 0}))
	out = append(out, a.event("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": u.CompletionTokens},
	}))
	out = append(out, a.event("message_stop", nil))
	return out, u
}

// ---------- 客户端: responses ----------

// responsesEventConverter Responses (SSE) 事件流输出。
// 客户端依赖完整的事件生命周期：output_item.added / content_part.added 宣告
// item_id 后，output_text.delta 才能被归属到对应条目（缺失时客户端报
// "text part msg_0 not found" 类错误），故事件顺序与官方 API 对齐。
type responsesEventConverter struct {
	streamConvBase
	seq       int64
	respID    string
	model     string
	partAdded bool
}

func (r *responsesEventConverter) event(typ string, fields map[string]any) []byte {
	m := map[string]any{"type": typ, "sequence_number": r.seq}
	r.seq++
	for k, v := range fields {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	return data
}

func (r *responsesEventConverter) responseObj(status string, output any) map[string]any {
	obj := map[string]any{"id": r.respID, "object": "response", "status": status, "output": output}
	if r.model != "" {
		obj["model"] = r.model
	}
	return obj
}

func (r *responsesEventConverter) Feed(data []byte) ([][]byte, error) {
	if err := r.parser.Feed(data); err != nil {
		return nil, err
	}
	var out [][]byte
	if !r.started {
		r.started = true
		r.respID = "resp_0"
		var head struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(data, &head)
		r.model = head.Model
		out = append(out,
			r.event("response.created", map[string]any{"response": r.responseObj("in_progress", []any{})}),
			r.event("response.in_progress", map[string]any{"response": r.responseObj("in_progress", []any{})}),
			r.event("response.output_item.added", map[string]any{
				"output_index": 0,
				"item": map[string]any{
					"id": "msg_0", "type": "message", "role": "assistant",
					"status": "in_progress", "content": []any{},
				},
			}),
		)
	}
	if suffix := r.pending(); suffix != "" {
		if !r.partAdded {
			r.partAdded = true
			out = append(out, r.event("response.content_part.added", map[string]any{
				"item_id": "msg_0", "output_index": 0, "content_index": 0,
				"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
			}))
		}
		out = append(out, r.event("response.output_text.delta", map[string]any{
			"delta": suffix, "item_id": "msg_0", "output_index": 0, "content_index": 0,
		}))
	}
	return out, nil
}

func (r *responsesEventConverter) Finalize() ([][]byte, Usage) {
	u := r.finalizeUsage()
	full := r.parser.Text()
	text := strings.TrimSpace(full)
	out := make([][]byte, 0, 6)
	if suffix := r.pending(); suffix != "" {
		out = append(out, r.event("response.output_text.delta", map[string]any{
			"delta": suffix, "item_id": "msg_0", "output_index": 0, "content_index": 0,
		}))
	}
	if r.partAdded {
		out = append(out,
			r.event("response.output_text.done", map[string]any{
				"item_id": "msg_0", "output_index": 0, "content_index": 0, "text": full,
			}),
			r.event("response.content_part.done", map[string]any{
				"item_id": "msg_0", "output_index": 0, "content_index": 0,
				"part": map[string]any{"type": "output_text", "text": text, "annotations": []any{}},
			}),
		)
	}
	item := map[string]any{
		"id": "msg_0", "type": "message", "role": "assistant", "status": "completed",
		"content": []map[string]any{{"type": "output_text", "text": text, "annotations": []any{}}},
	}
	out = append(out,
		r.event("response.output_item.done", map[string]any{"output_index": 0, "item": item}),
		r.event("response.completed", map[string]any{
			"response": func() map[string]any {
				resp := r.responseObj("completed", []map[string]any{item})
				resp["usage"] = map[string]any{
					"input_tokens": u.PromptTokens, "output_tokens": u.CompletionTokens, "total_tokens": u.TotalTokens,
					"input_tokens_details": map[string]any{"cached_tokens": u.CachedTokens},
				}
				return resp
			}(),
		}),
	)
	return out, u
}
