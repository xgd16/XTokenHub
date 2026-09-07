package gateway_test

import (
	"testing"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
)

// 会话标识：入站 X-Session-Id 应写入请求日志（session_id），并在
// started/completed 事件里透传——控制台首页按会话聚合依赖该字段。
func TestHandleCapturesSessionID(t *testing.T) {
	up := openAIUpstream(t)
	chs := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	chs.items[0].BaseURL = up.URL

	bus := eventbus.New()
	defer bus.Wait()
	startCh := subscribeEvent(bus, eventbus.EventRequestStarted, 1)
	doneCh := subscribeEvent(bus, eventbus.EventRequestCompleted, 1)
	logs := &memLogs{}
	exec := gateway.NewExecutor(chs, logs, bus, 10*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req := makeRequest(model.ProtocolChatCompletions, body, false)
	req.SessionID = "sess-2e0be4c9"
	exec.Handle(c.Request.Context(), w, req)

	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	if items[0].SessionID != "sess-2e0be4c9" {
		t.Errorf("日志 session_id = %q", items[0].SessionID)
	}

	// 事件负载须携带同一 session_id（前端实时行依赖）
	for typ, ch := range map[string]<-chan []eventbus.Event{"started": startCh, "completed": doneCh} {
		evts := waitEvents(t, ch, typ, 1)
		l, ok := evts[0].Payload.(*model.RequestLog)
		if !ok {
			t.Fatalf("%s 事件负载类型 = %T", typ, evts[0].Payload)
		}
		if l.SessionID != "sess-2e0be4c9" {
			t.Errorf("%s 事件 session_id = %q", typ, l.SessionID)
		}
	}
}

// 未携带会话头的调用方：session_id 留空，不得写入占位值。
func TestHandleWithoutSessionID(t *testing.T) {
	up := openAIUpstream(t)
	chs := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	chs.items[0].BaseURL = up.URL

	logs := &memLogs{}
	exec := gateway.NewExecutor(chs, logs, nil, 10*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	exec.Handle(c.Request.Context(), w, makeRequest(model.ProtocolChatCompletions, body, false))

	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	if items[0].SessionID != "" {
		t.Errorf("无会话头时 session_id 应为空, got %q", items[0].SessionID)
	}
}
