package gateway_test

import (
	"testing"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
)

// TestExecutorStampsKeyIntoLog 请求携带的调用方身份必须写入日志快照。
func TestExecutorStampsKeyIntoLog(t *testing.T) {
	srv := openAIUpstream(t)
	channels := &memChannels{items: []model.Channel{
		ch("oa", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}}
	channels.items[0].BaseURL = srv.URL

	bus := eventbus.New()
	defer bus.Wait()
	logs := &memLogs{}
	exec := gateway.NewExecutor(channels, logs, bus, 10*time.Second)

	c, w := ginCtx(t)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req := makeRequest(model.ProtocolChatCompletions, body, false)
	req.KeyID = 7
	req.KeyName = "agent-k"
	exec.Handle(c.Request.Context(), w, req)

	items := logs.snapshot()
	if len(items) != 1 {
		t.Fatalf("logs = %d", len(items))
	}
	lg := items[0]
	if lg.KeyID != 7 || lg.KeyName != "agent-k" {
		t.Errorf("log 缺少 key 身份: %+v", lg)
	}
	// 匿名请求（未设置 key）记录 0/""，区分匿名流量
	c2, w2 := ginCtx(t)
	exec.Handle(c2.Request.Context(), w2, makeRequest(model.ProtocolChatCompletions, body, false))
	lg2 := logs.snapshot()[1]
	if lg2.KeyID != 0 || lg2.KeyName != "" {
		t.Errorf("匿名请求 key 应为零值: %+v", lg2)
	}
}
