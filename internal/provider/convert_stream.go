package provider

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// Text 已累积的输出文本（供实时吞吐估算用；不含工具参数）。
	Text() string
	// ArgsText 已累积的工具调用参数 JSON 串（估算兜底用）。
	ArgsText() string
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

// upstreamFinish 上游结束原因，归一为 openai chat 语义（stop/length/tool_calls）。
func (b *streamConvBase) upstreamFinish() string {
	switch r := b.parser.FinishReason(); r {
	case "max_tokens", "length":
		return "length"
	case "tool_use", "tool_calls", "function_call":
		return "tool_calls"
	case "stop_sequence", "end_turn", "":
		return "stop"
	default:
		return r
	}
}

// Text 已累积的输出文本（不含工具参数）。
func (b *streamConvBase) Text() string { return b.parser.Text() }

// ArgsText 已累积的工具调用参数 JSON 串（估算兜底用）。
func (b *streamConvBase) ArgsText() string { return b.parser.ArgsText() }

// estText 全量估算文本（文本 + 工具参数）。
func (b *streamConvBase) estText() string { return b.parser.Text() + b.parser.ArgsText() }

// ---------- 上游工具调用事件归一 ----------

// upToolEvent 上游工具调用增量事件（chat/messages/responses 三协议归一）。
type upToolEvent struct {
	kind string // "start" 新调用宣告 / "args" 参数增量
	key  string // 同一调用的稳定键（chat 调用 index / anthropic 块 index / responses item_id）
	id   string // 调用 ID（start 时携带）
	name string // 函数名（start 时携带）
	args string // 参数增量（args 时携带）
}

// extractToolEvents 从上游 SSE 载荷提取工具调用事件；非工具事件返回 nil。
func extractToolEvents(data []byte) []upToolEvent {
	var raw struct {
		Type   string          `json:"type"`
		Delta  json.RawMessage `json:"delta"`
		ItemID string          `json:"item_id"`
		Item   *struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		} `json:"item"`
		Block *struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
		Index   *int64 `json:"index"`
		Choices []struct {
			Delta struct {
				ToolCalls []struct {
					Index    *int64 `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	intKey := func(i *int64) string {
		if i == nil {
			return "0"
		}
		return strconv.FormatInt(*i, 10)
	}
	var evs []upToolEvent
	// chat: choices[].delta.tool_calls（首个分片带 id/name，后续分片只带参数增量）
	for _, ch := range raw.Choices {
		for _, tc := range ch.Delta.ToolCalls {
			key := intKey(tc.Index)
			if tc.ID != "" || tc.Function.Name != "" {
				evs = append(evs, upToolEvent{kind: "start", key: key, id: tc.ID, name: tc.Function.Name})
			}
			if tc.Function.Arguments != "" {
				evs = append(evs, upToolEvent{kind: "args", key: key, args: tc.Function.Arguments})
			}
		}
	}
	switch raw.Type {
	case "content_block_start": // anthropic: tool_use 块宣告
		if raw.Block != nil && raw.Block.Type == "tool_use" {
			evs = append(evs, upToolEvent{kind: "start", key: intKey(raw.Index), id: raw.Block.ID, name: raw.Block.Name})
		}
	case "content_block_delta": // anthropic: input_json_delta 参数增量
		var d struct {
			Type        string `json:"type"`
			PartialJSON string `json:"partial_json"`
		}
		if json.Unmarshal(raw.Delta, &d) == nil && d.Type == "input_json_delta" && d.PartialJSON != "" {
			evs = append(evs, upToolEvent{kind: "args", key: intKey(raw.Index), args: d.PartialJSON})
		}
	case "response.output_item.added": // responses: function_call 条目宣告
		if raw.Item != nil && raw.Item.Type == "function_call" {
			evs = append(evs, upToolEvent{kind: "start", key: raw.Item.ID, id: raw.Item.CallID, name: raw.Item.Name})
		}
	case "response.function_call_arguments.delta": // responses: 参数增量
		if raw.ItemID != "" && raw.Delta != nil {
			var s string
			if json.Unmarshal(raw.Delta, &s) == nil && s != "" {
				evs = append(evs, upToolEvent{kind: "args", key: raw.ItemID, args: s})
			}
		}
	}
	return evs
}

func (b *streamConvBase) finalizeUsage() Usage {
	u, ok := b.parser.Usage()
	est := EstimateTokens(b.estText())
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
	id        string
	model     string
	createdAt int64
	tools     map[string]int // 上游调用键 -> chat tool_calls 数组 index
	toolN     int
	anyTool   bool
}

func (c *chatChunkConverter) chunk(delta string, finish *string, usage *map[string]any) []byte {
	if c.createdAt == 0 {
		c.createdAt = time.Now().Unix()
	}
	m := map[string]any{
		"id": c.id, "object": "chat.completion.chunk", "created": c.createdAt, "model": c.model,
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

// toolChunk 工具调用增量块（官方形态：delta.tool_calls 按 index 聚合）。
func (c *chatChunkConverter) toolChunk(calls []map[string]any) []byte {
	if c.createdAt == 0 {
		c.createdAt = time.Now().Unix()
	}
	m := map[string]any{
		"id": c.id, "object": "chat.completion.chunk", "created": c.createdAt, "model": c.model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": calls}}},
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
	}
	// 首块起持续解析 id/model（openai chat 在顶层；anthropic/responses 在嵌套对象里，
	// 且部分上游分块携带 id 的时机不定，故在拿到前反复尝试）
	if c.id == "" || c.model == "" {
		var head struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Message *struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"message"`
			Response *struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"response"`
		}
		_ = json.Unmarshal(data, &head)
		if head.ID != "" {
			c.id = head.ID
		} else if head.Message != nil && head.Message.ID != "" {
			c.id = head.Message.ID
		} else if head.Response != nil && head.Response.ID != "" {
			c.id = head.Response.ID
		}
		if head.Model != "" {
			c.model = head.Model
		} else if head.Message != nil && head.Message.Model != "" {
			c.model = head.Message.Model
		} else if head.Response != nil && head.Response.Model != "" {
			c.model = head.Response.Model
		}
	}
	var out [][]byte
	// 上游工具调用事件（anthropic tool_use / responses function_call）转 chat delta.tool_calls
	for _, ev := range extractToolEvents(data) {
		switch ev.kind {
		case "start":
			if _, ok := c.tools[ev.key]; ok {
				continue // 同一调用的重复宣告
			}
			if c.tools == nil {
				c.tools = map[string]int{}
			}
			idx := c.toolN
			c.toolN++
			c.tools[ev.key] = idx
			c.anyTool = true
			id := ev.id
			if id == "" {
				id = fmt.Sprintf("call_%d", idx+1)
			}
			out = append(out, c.toolChunk([]map[string]any{{
				"index": idx, "id": id, "type": "function",
				"function": map[string]any{"name": ev.name, "arguments": ""},
			}}))
		case "args":
			idx, ok := c.tools[ev.key]
			if !ok {
				continue
			}
			out = append(out, c.toolChunk([]map[string]any{{
				"index": idx, "function": map[string]any{"arguments": ev.args},
			}}))
		}
	}
	suffix := c.pending()
	if suffix != "" {
		out = append(out, c.chunk(suffix, nil, nil))
	}
	return out, nil
}

func (c *chatChunkConverter) Finalize() ([][]byte, Usage) {
	out := make([][]byte, 0, 3)
	if suffix := c.pending(); suffix != "" {
		out = append(out, c.chunk(suffix, nil, nil))
	}
	u := c.finalizeUsage()
	finish := c.upstreamFinish()
	// responses 上游无显式结束原因：本轮发起过工具调用时按官方语义补 tool_calls
	if finish == "stop" && c.anyTool {
		finish = "tool_calls"
	}
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
	msgID      string
	model      string
	textOpen   bool           // 文本块（index 0）是否仍开放
	openTool   int            // 当前开放的 tool_use 块 index（-1 无）
	nextBlock  int            // 下一个内容块 index（文本块占用 0，工具块顺延）
	toolBlocks map[string]int // 上游调用键 -> anthropic 块 index
	anyTool    bool
}

func (a *anthropicEventConverter) event(typ string, fields map[string]any) []byte {
	m := map[string]any{"type": typ}
	for k, v := range fields {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	return data
}

// head 从上游事件中提取 id/model（chat 在顶层，anthropic 在 message.*，
// responses 在 response.*）；官方 message_start 的 message.id/model 为必填。
func (a *anthropicEventConverter) head(data []byte) {
	if a.msgID != "" && a.model != "" {
		return
	}
	var h struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Message *struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"message"`
		Response *struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &h) != nil {
		return
	}
	pick := func(top, msg, resp string) string {
		if top != "" {
			return top
		}
		if msg != "" {
			return msg
		}
		return resp
	}
	if a.msgID == "" {
		var m, r string
		if h.Message != nil {
			m = h.Message.ID
		}
		if h.Response != nil {
			r = h.Response.ID
		}
		a.msgID = pick(h.ID, m, r)
	}
	if a.model == "" {
		var m, r string
		if h.Message != nil {
			m = h.Message.Model
		}
		if h.Response != nil {
			r = h.Response.Model
		}
		a.model = pick(h.Model, m, r)
	}
}

func (a *anthropicEventConverter) Feed(data []byte) ([][]byte, error) {
	if err := a.parser.Feed(data); err != nil {
		return nil, err
	}
	a.head(data)
	var out [][]byte
	if !a.started {
		a.started = true
		// message_start 的 message 对象官方为严格校验：id/model/usage 均必填
		u, ok := a.parser.Usage()
		if !ok {
			u = Usage{}
		}
		msg := map[string]any{
			"id": a.msgID, "type": "message", "role": "assistant", "model": a.model,
			"content": []any{},
			"usage": map[string]any{
				"input_tokens":                u.PromptTokens,
				"output_tokens":               0,
				"cache_read_input_tokens":     u.CachedTokens,
				"cache_creation_input_tokens": u.CacheWriteTokens,
			},
		}
		if a.msgID == "" {
			msg["id"] = "msg_0"
		}
		out = append(out, a.event("message_start", map[string]any{"message": msg}))
		out = append(out, a.event("content_block_start", map[string]any{
			"index": 0, "content_block": map[string]any{"type": "text", "text": ""},
		}))
		a.textOpen = true
		a.openTool = -1
		a.nextBlock = 1
	}
	// 上游工具调用事件（chat tool_calls / responses function_call）转 tool_use 块
	for _, ev := range extractToolEvents(data) {
		switch ev.kind {
		case "start":
			if _, ok := a.toolBlocks[ev.key]; ok {
				continue // 同一调用的重复宣告
			}
			if a.textOpen {
				// anthropic 块须顺序闭合：开工具块前先关文本块
				a.textOpen = false
				out = append(out, a.event("content_block_stop", map[string]any{"index": 0}))
			}
			if a.openTool >= 0 {
				out = append(out, a.event("content_block_stop", map[string]any{"index": a.openTool}))
			}
			if a.toolBlocks == nil {
				a.toolBlocks = map[string]int{}
			}
			b := a.nextBlock
			a.nextBlock++
			a.toolBlocks[ev.key] = b
			a.openTool = b
			a.anyTool = true
			id := ev.id
			if id == "" {
				id = fmt.Sprintf("toolu_%d", b)
			}
			out = append(out, a.event("content_block_start", map[string]any{
				"index": b, "content_block": map[string]any{"type": "tool_use", "id": id, "name": ev.name, "input": map[string]any{}},
			}))
		case "args":
			b, ok := a.toolBlocks[ev.key]
			if !ok {
				continue
			}
			out = append(out, a.event("content_block_delta", map[string]any{
				"index": b, "delta": map[string]any{"type": "input_json_delta", "partial_json": ev.args},
			}))
		}
	}
	if suffix := a.pending(); suffix != "" && !a.anyTool {
		// 文本块已因工具调用闭合后到达的残余文本不再回传（仍计入 usage 估算）
		out = append(out, a.event("content_block_delta", map[string]any{
			"index": 0, "delta": map[string]any{"type": "text_delta", "text": suffix},
		}))
	}
	return out, nil
}

func (a *anthropicEventConverter) Finalize() ([][]byte, Usage) {
	out := make([][]byte, 0, 3)
	if suffix := a.pending(); suffix != "" && !a.anyTool {
		out = append(out, a.event("content_block_delta", map[string]any{
			"index": 0, "delta": map[string]any{"type": "text_delta", "text": suffix},
		}))
	}
	u := a.finalizeUsage()
	// 收尾闭合仍开放的块（工具块优先，其次文本块；两者互斥不会同时开放）
	if a.openTool >= 0 {
		out = append(out, a.event("content_block_stop", map[string]any{"index": a.openTool}))
		a.openTool = -1
	} else if a.textOpen || !a.anyTool {
		out = append(out, a.event("content_block_stop", map[string]any{"index": 0}))
		a.textOpen = false
	}
	// 结束原因按上游映射（length 截断应为 max_tokens 而非 end_turn）
	stop := mapStop(a.upstreamFinish())
	if a.anyTool && stop != "max_tokens" {
		// responses 上游无显式结束原因：发起过工具调用时按官方语义补 tool_use
		stop = "tool_use"
	}
	// input_tokens 补报：chat 上游的 prompt_tokens 流末才到，message_start 已发出，
	// 故在 message_delta 的 usage 中补齐（官方 MessageDeltaUsage 支持该字段）。
	out = append(out, a.event("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil},
		"usage": map[string]any{
			"input_tokens":  u.PromptTokens,
			"output_tokens": u.CompletionTokens,
		},
	}))
	out = append(out, a.event("message_stop", nil))
	return out, u
}

// ---------- 客户端: responses ----------

// responsesFC 进行中的 function_call 输出条目。
type responsesFC struct {
	itemID   string
	callID   string
	name     string
	args     strings.Builder
	outIndex int64
}

// responsesEventConverter Responses (SSE) 事件流输出。
// 客户端依赖完整的事件生命周期：output_item.added / content_part.added 宣告
// item_id 后，output_text.delta 才能被归属到对应条目（缺失时客户端报
// "text part msg_0 not found" 类错误），故事件顺序与官方 API 对齐。
// message 条目惰性宣告（首个文本增量时）——纯工具调用响应不产生空 message 条目；
// function_call 条目经 output_item.added / function_call_arguments.delta / done
// / output_item.done 完整转译上游工具调用。
// response 对象还须携带 created_at/parallel_tool_calls/tool_choice/tools 等
// 必填字段（官方 SDK 以 pydantic 严格校验，缺一即解析失败）。
type responsesEventConverter struct {
	streamConvBase
	seq       int64
	respID    string
	model     string
	createdAt int64
	partAdded bool           // output_text part 是否已宣告
	msgAdded  bool           // message 条目是否已宣告
	msgOut    int64          // message 条目的 output_index
	nextOut   int64          // 下一个 output_index
	calls     []*responsesFC // 已宣告的 function_call 条目（宣告序）
	callByKey map[string]*responsesFC
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

// ensureCreated 首次构造 response 对象时固定 created_at。
func (r *responsesEventConverter) ensureCreated() {
	if r.createdAt == 0 {
		r.createdAt = time.Now().Unix()
	}
}

func (r *responsesEventConverter) responseObj(status string, output any) map[string]any {
	r.ensureCreated()
	if output == nil {
		output = []any{}
	}
	return map[string]any{
		"id":                  r.respID,
		"object":              "response",
		"created_at":          r.createdAt,
		"status":              status,
		"model":               r.model,
		"output":              output,
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"tools":               []any{},
		"text":                map[string]any{"format": map[string]any{"type": "text"}},
		"truncation":          "disabled",
		"metadata":            map[string]any{},
		"error":               nil,
		"incomplete_details":  nil,
	}
}

// usageObj responses usage 形态（input/output 明细字段官方 SDK 亦为必填）。
func responsesUsageObj(u Usage) map[string]any {
	return map[string]any{
		"input_tokens":  u.PromptTokens,
		"output_tokens": u.CompletionTokens,
		"total_tokens":  u.TotalTokens,
		"input_tokens_details": map[string]any{
			"cached_tokens":      u.CachedTokens,
			"cache_write_tokens": u.CacheWriteTokens,
		},
		"output_tokens_details": map[string]any{"reasoning_tokens": 0},
	}
}

// fcItem function_call 输出条目（官方必填：id/call_id/name/arguments/status）。
func (r *responsesEventConverter) fcItem(fc *responsesFC, status string) map[string]any {
	callID := fc.callID
	if callID == "" {
		callID = fc.itemID
	}
	return map[string]any{
		"id": fc.itemID, "type": "function_call", "call_id": callID,
		"name": fc.name, "arguments": fc.args.String(), "status": status,
	}
}

// ensureMsg 惰性宣告 message 输出条目（首个文本增量时）。
func (r *responsesEventConverter) ensureMsg(out [][]byte) [][]byte {
	if r.msgAdded {
		return out
	}
	r.msgAdded = true
	r.msgOut = r.nextOut
	r.nextOut++
	return append(out, r.event("response.output_item.added", map[string]any{
		"output_index": r.msgOut,
		"item": map[string]any{
			"id": "msg_0", "type": "message", "role": "assistant",
			"status": "in_progress", "content": []any{},
		},
	}))
}

func (r *responsesEventConverter) Feed(data []byte) ([][]byte, error) {
	if err := r.parser.Feed(data); err != nil {
		return nil, err
	}
	var out [][]byte
	if !r.started {
		r.started = true
		r.respID = "resp_0"
		// 持续提取上游 id/model（responses 事件在 response.* 嵌套，anthropic 在 message.*）
		var head struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Message *struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"message"`
			Response *struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"response"`
		}
		_ = json.Unmarshal(data, &head)
		if head.Response != nil && head.Response.ID != "" {
			r.respID = head.Response.ID
		} else if head.ID != "" {
			r.respID = head.ID
		} else if head.Message != nil && head.Message.ID != "" {
			r.respID = head.Message.ID
		}
		if head.Response != nil && head.Response.Model != "" {
			r.model = head.Response.Model
		} else if head.Model != "" {
			r.model = head.Model
		} else if head.Message != nil && head.Message.Model != "" {
			r.model = head.Message.Model
		}
		out = append(out,
			r.event("response.created", map[string]any{"response": r.responseObj("in_progress", []any{})}),
			r.event("response.in_progress", map[string]any{"response": r.responseObj("in_progress", []any{})}),
		)
	}
	// 上游工具调用事件（chat tool_calls / anthropic tool_use 块）转 function_call 条目
	for _, ev := range extractToolEvents(data) {
		switch ev.kind {
		case "start":
			if _, ok := r.callByKey[ev.key]; ok {
				continue // 同一调用的重复宣告
			}
			if r.callByKey == nil {
				r.callByKey = map[string]*responsesFC{}
			}
			fc := &responsesFC{
				itemID:   fmt.Sprintf("fc_%d", len(r.calls)),
				callID:   ev.id,
				name:     ev.name,
				outIndex: r.nextOut,
			}
			r.nextOut++
			r.callByKey[ev.key] = fc
			r.calls = append(r.calls, fc)
			out = append(out, r.event("response.output_item.added", map[string]any{
				"output_index": fc.outIndex, "item": r.fcItem(fc, "in_progress"),
			}))
		case "args":
			fc, ok := r.callByKey[ev.key]
			if !ok {
				continue
			}
			fc.args.WriteString(ev.args)
			out = append(out, r.event("response.function_call_arguments.delta", map[string]any{
				"item_id": fc.itemID, "output_index": fc.outIndex, "delta": ev.args,
			}))
		}
	}
	if suffix := r.pending(); suffix != "" {
		out = r.ensureMsg(out)
		if !r.partAdded {
			r.partAdded = true
			out = append(out, r.event("response.content_part.added", map[string]any{
				"item_id": "msg_0", "output_index": r.msgOut, "content_index": 0,
				"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}, "logprobs": []any{}},
			}))
		}
		out = append(out, r.event("response.output_text.delta", map[string]any{
			"delta": suffix, "item_id": "msg_0", "output_index": r.msgOut, "content_index": 0,
			"logprobs": []any{},
		}))
	}
	return out, nil
}

func (r *responsesEventConverter) Finalize() ([][]byte, Usage) {
	u := r.finalizeUsage()
	full := r.parser.Text()
	out := make([][]byte, 0, 8)
	if suffix := r.pending(); suffix != "" {
		out = r.ensureMsg(out)
		out = append(out, r.event("response.output_text.delta", map[string]any{
			"delta": suffix, "item_id": "msg_0", "output_index": r.msgOut, "content_index": 0,
			"logprobs": []any{},
		}))
	}
	// 纯文本/空响应：确保 message 条目存在（纯工具调用响应则不产生）
	if !r.msgAdded && len(r.calls) == 0 {
		out = r.ensureMsg(out)
	}
	// 各条目按 output_index 顺序收尾（item done 事件须有序）
	type closer struct {
		idx int64
		evs [][]byte
	}
	var closers []closer
	if r.msgAdded {
		var evs [][]byte
		if r.partAdded {
			evs = append(evs,
				r.event("response.output_text.done", map[string]any{
					"item_id": "msg_0", "output_index": r.msgOut, "content_index": 0,
					"text": full, "logprobs": []any{},
				}),
				r.event("response.content_part.done", map[string]any{
					"item_id": "msg_0", "output_index": r.msgOut, "content_index": 0,
					"part": map[string]any{"type": "output_text", "text": full, "annotations": []any{}, "logprobs": []any{}},
				}),
			)
		}
		// 结束原因 length 对应官方 incomplete（截断）而非 completed
		itemStatus := "completed"
		if r.upstreamFinish() == "length" {
			itemStatus = "incomplete"
		}
		content := any([]map[string]any{{"type": "output_text", "text": full, "annotations": []any{}, "logprobs": []any{}}})
		if !r.partAdded {
			content = []any{}
		}
		evs = append(evs, r.event("response.output_item.done", map[string]any{
			"output_index": r.msgOut,
			"item": map[string]any{
				"id": "msg_0", "type": "message", "role": "assistant", "status": itemStatus,
				"content": content,
			},
		}))
		closers = append(closers, closer{r.msgOut, evs})
	}
	for _, fc := range r.calls {
		evs := [][]byte{
			r.event("response.function_call_arguments.done", map[string]any{
				"item_id": fc.itemID, "output_index": fc.outIndex, "arguments": fc.args.String(),
			}),
			r.event("response.output_item.done", map[string]any{
				"output_index": fc.outIndex, "item": r.fcItem(fc, "completed"),
			}),
		}
		closers = append(closers, closer{fc.outIndex, evs})
	}
	sort.Slice(closers, func(i, j int) bool { return closers[i].idx < closers[j].idx })
	for _, c := range closers {
		out = append(out, c.evs...)
	}
	// 终态事件与 response 对象（output 条目按 output_index 排列）
	objStatus, terminal := "completed", "response.completed"
	if r.upstreamFinish() == "length" {
		objStatus, terminal = "incomplete", "response.incomplete"
	}
	output := make([]any, 0, len(r.calls)+1)
	for _, c := range closers { // closers 已按 output_index 升序
		if c.idx == r.msgOut && r.msgAdded {
			content := any([]map[string]any{{"type": "output_text", "text": full, "annotations": []any{}, "logprobs": []any{}}})
			if !r.partAdded {
				content = []any{}
			}
			output = append(output, map[string]any{
				"id": "msg_0", "type": "message", "role": "assistant", "status": objStatus,
				"content": content,
			})
		} else {
			for _, fc := range r.calls {
				if fc.outIndex == c.idx {
					output = append(output, r.fcItem(fc, "completed"))
					break
				}
			}
		}
	}
	resp := r.responseObj(objStatus, output)
	if terminal == "response.incomplete" {
		resp["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	}
	resp["usage"] = responsesUsageObj(u)
	out = append(out, r.event(terminal, map[string]any{"response": resp}))
	return out, u
}
