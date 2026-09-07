package gateway_test

import (
	"context"
	"sync"
	"testing"

	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
)

// memChannels 内存渠道源。
type memChannels struct{ items []model.Channel }

func (m *memChannels) ListEnabled(context.Context) ([]model.Channel, error) { return m.items, nil }

// memLogs 内存日志收集器。
type memLogs struct {
	mu    sync.Mutex
	items []*model.RequestLog
}

func (m *memLogs) Create(_ context.Context, l *model.RequestLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *l
	cp.ID = int64(len(m.items) + 1)
	m.items = append(m.items, &cp)
	return nil
}

func (m *memLogs) snapshot() []*model.RequestLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*model.RequestLog, len(m.items))
	copy(out, m.items)
	return out
}

func ch(name string, providerType model.ProviderType, priority, weight int, status model.ChannelStatus, models, protocols string) model.Channel {
	return model.Channel{
		Name: name, Provider: providerType, BaseURL: "https://upstream.test", APIKey: "k",
		Priority: priority, Weight: weight, Status: status, Models: models, Protocols: protocols,
	}
}

func TestSelectCandidatesNativeFirst(t *testing.T) {
	channels := []model.Channel{
		ch("conv-best-priority", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
		ch("native-lower-priority", model.ProviderOpenAICompatible, 100, 1, model.ChannelEnabled, "gpt-4o", "chat_completions,responses"),
	}
	// 入站 responses：native-lower-priority 原生支持 -> 排最前（即使 priority 劣势）
	cands := gateway.SelectCandidates(channels, model.ProtocolResponses, "gpt-4o", nil)
	if len(cands) != 2 {
		t.Fatalf("candidates = %d", len(cands))
	}
	if cands[0].Channel.Name != "native-lower-priority" || cands[0].Mode != model.ForwardNativePassthrough {
		t.Errorf("原生渠道应排最前: %+v", cands[0])
	}
	if cands[1].Mode != model.ForwardConverted {
		t.Errorf("第二候选应为转换模式: %+v", cands[1])
	}
}

func TestSelectCandidatesFiltering(t *testing.T) {
	channels := []model.Channel{
		ch("disabled", model.ProviderOpenAICompatible, 1, 1, model.ChannelDisabled, "gpt-4o", "chat_completions"),
		ch("model-mismatch", model.ProviderAnthropic, 2, 1, model.ChannelEnabled, "claude-3", "messages"),
		ch("ok", model.ProviderOpenAICompatible, 3, 1, model.ChannelEnabled, "gpt-4o,other", "chat_completions"),
	}
	cands := gateway.SelectCandidates(channels, model.ProtocolChatCompletions, "gpt-4o", nil)
	if len(cands) != 1 || cands[0].Channel.Name != "ok" {
		t.Fatalf("过滤错误: %+v", cands)
	}
}

func TestSelectCandidatesPriorityOrder(t *testing.T) {
	// 全部原生：按 priority 升序；不同 priority 保证确定性
	channels := []model.Channel{
		ch("p50", model.ProviderOpenAICompatible, 50, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
		ch("p10", model.ProviderOpenAICompatible, 10, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
		ch("p20", model.ProviderOpenAICompatible, 20, 1, model.ChannelEnabled, "gpt-4o", "chat_completions"),
	}
	cands := gateway.SelectCandidates(channels, model.ProtocolChatCompletions, "gpt-4o", nil)
	want := []string{"p10", "p20", "p50"}
	for i, name := range want {
		if cands[i].Channel.Name != name {
			t.Errorf("cands[%d] = %s, want %s", i, cands[i].Channel.Name, name)
		}
	}
}

func TestSelectCandidatesWeightDistribution(t *testing.T) {
	// 同 priority 双渠道 weight 9:1 —— 统计 200 次选择首位分布
	channels := []model.Channel{
		ch("heavy", model.ProviderOpenAICompatible, 1, 9, model.ChannelEnabled, "m", "chat_completions"),
		ch("light", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "m", "chat_completions"),
	}
	heavyFirst := 0
	for i := 0; i < 200; i++ {
		cands := gateway.SelectCandidates(channels, model.ProtocolChatCompletions, "m", nil)
		if cands[0].Channel.Name == "heavy" {
			heavyFirst++
		}
	}
	if heavyFirst < 120 || heavyFirst > 190 {
		t.Errorf("weight 9:1 分布异常: heavy 先选 %d/200", heavyFirst)
	}
}

func TestSSESplitterEvents(t *testing.T) {
	var got [][]byte
	s := gateway.NewSSESplitter(func(data []byte) {
		cp := make([]byte, len(data))
		copy(cp, data)
		got = append(got, cp)
	})
	input := "event: message_start\r\ndata: {\"type\":\"message_start\"}\r\n\r\n" +
		": keep-alive\n\n" +
		"data: {\"delta\":\"a\"}\n\ndata: [DONE]\n\ndata: tail-no-newline"
	if _, err := s.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	s.Flush()

	if len(got) != 3 {
		t.Fatalf("events = %d: %v", len(got), got)
	}
	if string(got[0]) != `{"type":"message_start"}` {
		t.Errorf("event0 = %s", got[0])
	}
	if string(got[1]) != `{"delta":"a"}` {
		t.Errorf("event1 = %s（应过滤 [DONE]）", got[1])
	}
	// 无终结空行的残余 data 行在 Flush 时派发
	if string(got[2]) != "tail-no-newline" {
		t.Errorf("event2 = %s", got[2])
	}
}

// makeRequest 构造网关请求。
func makeRequest(p model.Protocol, body string, stream bool) *gateway.Request {
	conv, params, err := provider.ParseRequest(p, []byte(body))
	if err != nil {
		panic(err)
	}
	params.Stream = stream
	return &gateway.Request{Protocol: p, Conv: &conv, Params: params, Body: []byte(body), ClientIP: "1.2.3.4"}
}

// memCustoms 内存自定义模型组源。
type memCustoms struct{ items []model.CustomModel }

func (m *memCustoms) ListEnabled(context.Context) ([]model.CustomModel, error) { return m.items, nil }

// TestSelectCandidatesGroupRouting 自定义模型组路由：
// 成员优先级（越小越优先）-> 渠道优先级 -> 原生优先；候选携带成员模型作为上游模型。
func TestSelectCandidatesGroupRouting(t *testing.T) {
	channels := []model.Channel{
		ch("free-ch-p5", model.ProviderOpenAICompatible, 5, 1, model.ChannelEnabled, "glm-flash,deepseek-free", "chat_completions"),
		ch("paid-ch-p1", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "deepseek-chat", "chat_completions"),
		ch("conv-ch", model.ProviderAnthropic, 1, 1, model.ChannelEnabled, "glm-flash", "messages"),
	}
	group := model.CustomModel{Name: "free-1M", Status: model.ChannelEnabled}
	group.SetMembers([]model.ModelMember{
		{Model: "deepseek-chat", Priority: 10},
		{Model: "deepseek-free", Priority: 1},
		{Model: "glm-flash", Priority: 0},
	})

	cands := gateway.SelectCandidates(channels, model.ProtocolChatCompletions, "free-1M", &group)
	if len(cands) != 4 {
		t.Fatalf("candidates = %d: %+v", len(cands), cands)
	}
	// P0 成员全部在前（成员优先级主导），P10 成员最后；
	// 同成员内按渠道 priority 升序；同渠道内原生（透传）优先于转换。
	want := []struct {
		chName  string
		upModel string
		mode    model.ForwardMode
	}{
		{"conv-ch", "glm-flash", model.ForwardConverted},            // 渠道 P1，非原生
		{"free-ch-p5", "glm-flash", model.ForwardNativePassthrough}, // 渠道 P5，原生
		{"free-ch-p5", "deepseek-free", model.ForwardNativePassthrough},
		{"paid-ch-p1", "deepseek-chat", model.ForwardNativePassthrough},
	}
	for i, w := range want {
		if cands[i].Channel.Name != w.chName || cands[i].UpstreamModel != w.upModel || cands[i].Mode != w.mode {
			t.Errorf("cands[%d] = {%s %s %v}, want {%s %s %v}", i,
				cands[i].Channel.Name, cands[i].UpstreamModel, cands[i].Mode, w.chName, w.upModel, w.mode)
		}
	}
}

// 成员模型在所有渠道均无支持时不应产生候选，其余成员照常路由。
func TestSelectCandidatesGroupPartialSupport(t *testing.T) {
	channels := []model.Channel{
		ch("only-b", model.ProviderOpenAICompatible, 1, 1, model.ChannelEnabled, "model-b", "chat_completions"),
	}
	group := model.CustomModel{Name: "mix"}
	group.SetMembers([]model.ModelMember{{Model: "model-a"}, {Model: "model-b"}})
	cands := gateway.SelectCandidates(channels, model.ProtocolChatCompletions, "mix", &group)
	if len(cands) != 1 || cands[0].UpstreamModel != "model-b" {
		t.Fatalf("candidates = %+v", cands)
	}
}
