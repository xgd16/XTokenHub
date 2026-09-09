package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

// mockUpstream 实现 messages 与 responses 端点的假上游，并附带 /v1/models 模型列表。
func mockUpstream(t *testing.T, native map[model.Protocol]bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	resp := map[model.Protocol]string{
		model.ProtocolChatCompletions: `{"id":"c","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		model.ProtocolResponses:       `{"id":"r","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`,
		model.ProtocolMessages:        `{"id":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`,
	}
	for _, p := range model.AllProtocols {
		path := "/v1/" + provider.EndpointPath(p)
		ok := native[p]
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(resp[p]))
		})
	}
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"probe-auto-mini"},{"id":"probe-auto-max"},{"id":"probe-auto-mini"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newChannelService(t *testing.T) (*service.ChannelService, *eventbus.Bus, context.Context) {
	t.Helper()
	db := testutil.NewMemoryDB(t)
	bus := eventbus.New()
	prober := provider.NewProber(2 * time.Second)
	svc := service.NewChannelService(repository.NewChannelRepository(db), prober, bus)
	return svc, bus, context.Background()
}

func TestChannelServiceCreateDuplicateName(t *testing.T) {
	svc, _, ctx := newChannelService(t)
	in := &service.CreateInput{Name: "a", Provider: model.ProviderOpenAICompatible, BaseURL: "https://x.com", APIKey: "k"}
	if _, err := svc.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, in); err == nil {
		t.Error("重名应报错")
	}
}

func TestChannelServiceProbe(t *testing.T) {
	srv := mockUpstream(t, map[model.Protocol]bool{
		model.ProtocolChatCompletions: true,
		model.ProtocolMessages:        true,
	})
	svc, bus, ctx := newChannelService(t)
	evCh := make(chan eventbus.Event, 1)
	bus.Subscribe(eventbus.EventChannelProbeResult, func(e eventbus.Event) { evCh <- e })

	in := &service.CreateInput{Name: "probe-me", Provider: model.ProviderOpenAICompatible, BaseURL: srv.URL, APIKey: "k"}
	ch, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	report, err := svc.Probe(ctx, ch.ID, "gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.NativeProtocols) != 2 {
		t.Fatalf("native = %v", report.NativeProtocols)
	}
	// 库已更新
	updated, _ := svc.Get(ctx, ch.ID)
	if !updated.IsNative(model.ProtocolMessages) || !updated.IsNative(model.ProtocolChatCompletions) {
		t.Errorf("native_protocols 未落库: %q", updated.Protocols)
	}
	if updated.LastProbeAt == nil {
		t.Error("last_probe_at 未更新")
	}
	if updated.ProbeResult == "" {
		t.Error("probe_result 未记录")
	}
	select {
	case e := <-evCh:
		payload := e.Payload.(map[string]any)
		if payload["channel_id"] != ch.ID {
			t.Errorf("事件 channel_id = %v", payload["channel_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("探测事件未发布")
	}
}

// 渠道未配置支持模型（留空=全部）时，探测自动选用上游模型列表中的真实模型。
func TestChannelServiceProbeAutoPickModel(t *testing.T) {
	srv := mockUpstream(t, map[model.Protocol]bool{model.ProtocolChatCompletions: true})
	svc, _, ctx := newChannelService(t)
	ch, err := svc.Create(ctx, &service.CreateInput{Name: "auto", Provider: model.ProviderOpenAICompatible, BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.Probe(ctx, ch.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.ProbeModel != "probe-auto-mini" {
		t.Errorf("应自动选用上游列表第一个模型, got %q", report.ProbeModel)
	}
	if len(report.NativeProtocols) != 1 || report.NativeProtocols[0] != model.ProtocolChatCompletions {
		t.Errorf("native = %v", report.NativeProtocols)
	}
}

// 探测模型只取渠道最终保存的支持模型（第一项）；留空（支持全部）时才自动发现：
// 上游模型列表 -> 主域名兜底列表 -> 内置兜底。
func TestChannelServiceProbeModelSource(t *testing.T) {
	noModelsUpstream := func(t *testing.T) *httptest.Server {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"no models api"}}`))
		})
		mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"c","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return srv
	}
	svc, _, ctx := newChannelService(t)

	// ① 渠道配置了支持模型：即使上游有自己的模型列表，也只用用户保存的模型
	srv := mockUpstream(t, map[model.Protocol]bool{model.ProtocolChatCompletions: true})
	ch, _ := svc.Create(ctx, &service.CreateInput{
		Name: "fb-1", Provider: model.ProviderOpenAICompatible, BaseURL: srv.URL, APIKey: "k",
		Models: []string{"chan-model-a", "chan-model-b"},
	})
	report, err := svc.Probe(ctx, ch.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.ProbeModel != "chan-model-a" {
		t.Errorf("应只用用户保存的支持模型, got %q", report.ProbeModel)
	}

	// ② 用户留空（支持全部）→ 自动用上游模型列表
	srv2 := mockUpstream(t, map[model.Protocol]bool{model.ProtocolChatCompletions: true})
	ch2, _ := svc.Create(ctx, &service.CreateInput{
		Name: "fb-2", Provider: model.ProviderOpenAICompatible, BaseURL: srv2.URL, APIKey: "k",
	})
	report2, err := svc.Probe(ctx, ch2.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if report2.ProbeModel != "probe-auto-mini" {
		t.Errorf("留空时应自动选用上游列表第一个模型, got %q", report2.ProbeModel)
	}

	// ③ 用户留空且上游无模型列表接口 → 主域名兜底 -> 内置兜底
	srv3 := noModelsUpstream(t)
	ch3, _ := svc.Create(ctx, &service.CreateInput{
		Name: "fb-3", Provider: model.ProviderOpenAICompatible, BaseURL: srv3.URL, APIKey: "k",
	})
	report3, err := svc.Probe(ctx, ch3.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if report3.ProbeModel != provider.DefaultProbeModel[model.ProviderOpenAICompatible] {
		t.Errorf("应退回内置兜底模型, got %q", report3.ProbeModel)
	}
}

// LookupModels 用给定凭据拉取上游模型列表（无需渠道已创建）。
func TestChannelServiceLookupModels(t *testing.T) {
	srv := mockUpstream(t, map[model.Protocol]bool{})
	svc, _, ctx := newChannelService(t)
	ms, err := svc.LookupModels(ctx, model.ProviderOpenAICompatible, srv.URL, "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0] != "probe-auto-mini" || ms[1] != "probe-auto-max" {
		t.Errorf("应去重并保持顺序, got %v", ms)
	}
	if _, err := svc.LookupModels(ctx, model.ProviderOpenAICompatible, "ftp://x", "k"); err == nil {
		t.Error("非法 BaseURL 应报错")
	}
}

// DeepSeek 场景：/messages 渠道 BaseURL 带子路径（如 https://api.deepseek.com/anthropic），
// 子路径无 /v1/models 时探测选模应降级用主域名列表，端点拼装保持 子路径 + /v1/messages。
func TestChannelServiceProbeSubPathBase(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/anthropic/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid URL"}}`))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`))
	})
	mux.HandleFunc("/anthropic/v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	})
	srv := httptest.NewServer(mux)

	svc, _, ctx := newChannelService(t)
	ch, err := svc.Create(ctx, &service.CreateInput{
		Name: "deepseek-anthropic", Provider: model.ProviderAnthropic, BaseURL: srv.URL + "/anthropic", APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.Probe(ctx, ch.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.ProbeModel != "deepseek-chat" {
		t.Errorf("应降级选主域名列表模型, got %q", report.ProbeModel)
	}
	// 子路径是协议挂载点：只探测 messages 家族协议
	if len(report.Items) != 1 || report.Items[0].Protocol != model.ProtocolMessages || !report.Items[0].OK {
		t.Fatalf("items = %+v", report.Items)
	}
	if len(report.NativeProtocols) != 1 || report.NativeProtocols[0] != model.ProtocolMessages {
		t.Errorf("native = %v", report.NativeProtocols)
	}
}

// 接口风格自动推断：省略 provider 时按 BaseURL 推断，显式非法值报错。
func TestChannelProviderAutoInfer(t *testing.T) {
	svc, _, ctx := newChannelService(t)

	// 挂载点含 anthropic → Anthropic 风格
	ch, err := svc.Create(ctx, &service.CreateInput{Name: "auto-a", BaseURL: "https://api.deepseek.com/anthropic", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Provider != model.ProviderAnthropic {
		t.Errorf("应推断为 Anthropic 风格, got %q", ch.Provider)
	}

	// 官方 Anthropic 域名
	ch2, _ := svc.Create(ctx, &service.CreateInput{Name: "auto-b", BaseURL: "https://api.anthropic.com", APIKey: "k"})
	if ch2.Provider != model.ProviderAnthropic {
		t.Errorf("api.anthropic.com 应推断为 Anthropic 风格, got %q", ch2.Provider)
	}

	// 裸域名 → OpenAI 兼容
	ch3, _ := svc.Create(ctx, &service.CreateInput{Name: "auto-c", BaseURL: "https://api.deepseek.com", APIKey: "k"})
	if ch3.Provider != model.ProviderOpenAICompatible {
		t.Errorf("应推断为 OpenAI 兼容, got %q", ch3.Provider)
	}

	// 显式指定覆盖推断
	ch4, _ := svc.Create(ctx, &service.CreateInput{
		Name: "auto-d", Provider: model.ProviderOpenAICompatible, BaseURL: "https://x.com/anthropic", APIKey: "k",
	})
	if ch4.Provider != model.ProviderOpenAICompatible {
		t.Errorf("显式指定应覆盖推断, got %q", ch4.Provider)
	}

	// 显式非法值报错
	if _, err := svc.Create(ctx, &service.CreateInput{Name: "auto-e", Provider: "nope", BaseURL: "https://x.com", APIKey: "k"}); err == nil {
		t.Error("非法接口风格应报错")
	}
}

func TestChannelServiceUpdate(t *testing.T) {
	svc, _, ctx := newChannelService(t)
	ch, _ := svc.Create(ctx, &service.CreateInput{Name: "u1", Provider: model.ProviderAnthropic, BaseURL: "https://a.com", APIKey: "k"})

	updated, err := svc.Update(ctx, ch.ID, &service.CreateInput{
		Name: "u1", Provider: model.ProviderAnthropic, BaseURL: "https://b.com", APIKey: "k2",
		Status: ptrStatus(model.ChannelDisabled),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.BaseURL != "https://b.com" || updated.APIKey != "k2" || updated.Status != model.ChannelDisabled {
		t.Errorf("update 未生效: %+v", updated)
	}

	// 更新为重名应报错
	svc.Create(ctx, &service.CreateInput{Name: "u2", Provider: model.ProviderAnthropic, BaseURL: "https://a.com", APIKey: "k"})
	if _, err := svc.Update(ctx, ch.ID, &service.CreateInput{Name: "u2", Provider: model.ProviderAnthropic, BaseURL: "https://a.com", APIKey: "k"}); err == nil {
		t.Error("更新为重名应报错")
	}
}

func TestLogAndStatsService(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	logRepo := repository.NewRequestLogRepository(db)
	now := time.Now()
	for _, l := range []model.RequestLog{
		{Model: "gpt-4o", ChannelName: "a", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, PromptTokens: 10, CompletionTokens: 5, CachedTokens: 5, DurationMS: 100, CreatedAt: now},
		{Model: "claude-3", ChannelName: "b", Protocol: model.ProtocolMessages, ForwardMode: model.ForwardConverted, PromptTokens: 20, CompletionTokens: 5, DurationMS: 300, CreatedAt: now, Error: "x"},
	} {
		if err := logRepo.Create(context.Background(), &l); err != nil {
			t.Fatal(err)
		}
	}

	logSvc := service.NewLogService(logRepo)
	items, total, err := logSvc.List(context.Background(), service.ListInput{Model: "gpt-4o"}, pagination.Normalize(1, 20))
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("LogService.List: %v %d", err, total)
	}

	statsSvc := service.NewStatsService(logRepo)
	s, err := statsSvc.Summary(context.Background(), 24)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalRequests != 2 || s.SuccessReqs != 1 {
		t.Errorf("summary: %+v", s)
	}
	if _, err := statsSvc.TrendByDay(context.Background(), 7); err != nil {
		t.Errorf("trend: %v", err)
	}
	if _, err := statsSvc.ByModel(context.Background(), 24); err != nil {
		t.Errorf("by-model: %v", err)
	}
	if _, err := statsSvc.ByChannel(context.Background(), 24); err != nil {
		t.Errorf("by-channel: %v", err)
	}

	// 长会话端到端：会话合计覆盖全量历史，不因前端只展示最近若干组而残缺。
	for i := 0; i < 108; i++ {
		l := model.RequestLog{
			SessionID: "long", Model: "glm-5.3-flash", ChannelName: "c",
			Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough,
			PromptTokens: 100_000, CompletionTokens: 30_000, CachedTokens: 99_000,
			DurationMS: 10_000, CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		if err := logRepo.Create(context.Background(), &l); err != nil {
			t.Fatal(err)
		}
	}
	sessions, err := statsSvc.LiveSessions(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	var long *repository.LiveSession
	for i := range sessions {
		if sessions[i].SessionID == "long" {
			long = &sessions[i]
		}
	}
	if long == nil {
		t.Fatal("未返回 long 会话")
	}
	if long.Requests != 108 {
		t.Errorf("long.Requests = %d, want 108", long.Requests)
	}
	if long.TotalTokens != 108*130_000 {
		t.Errorf("long.TotalTokens = %d, want %d", long.TotalTokens, 108*130_000)
	}
}

// 序列化烟测：模型 JSON 标签符合前端契约。
func TestModelJSONShape(t *testing.T) {
	ch := model.Channel{Name: "x", Provider: model.ProviderOpenAICompatible}
	b, _ := json.Marshal(ch)
	for _, key := range []string{"name", "provider", "base_url", "api_key", "native_protocols", "priority", "weight", "status"} {
		if !jsonContains(b, key) {
			t.Errorf("缺少字段 %s: %s", key, b)
		}
	}
	lg := model.RequestLog{Protocol: model.ProtocolMessages, ForwardMode: model.ForwardConverted}
	b2, _ := json.Marshal(lg)
	for _, key := range []string{"protocol", "forward_mode", "prompt_tokens", "completion_tokens", "cached_tokens", "cache_write_tokens", "cache_hit_rate", "duration_ms", "upstream_status"} {
		if !jsonContains(b2, key) {
			t.Errorf("缺少字段 %s: %s", key, b2)
		}
	}
}

func jsonContains(b []byte, key string) bool {
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	_, ok := m[key]
	return ok
}

func ptrStatus(s model.ChannelStatus) *model.ChannelStatus { return &s }

// 自定义模型组 CRUD：唯一名、成员去重规范化、更新校验、分页列表与删除。
func TestCustomModelServiceCRUD(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	svc := service.NewCustomModelService(repository.NewCustomModelRepository(db))
	ctx := context.Background()

	cm, err := svc.Create(ctx, &service.CustomModelInput{
		Name: "free-1M",
		Members: []model.ModelMember{
			{Model: "glm-flash", Priority: 0},
			{Model: "glm-flash", Priority: 5}, // 重复，应去重
			{Model: " deepseek-free "},        // 带空白，应 trim
			{Model: "   "},                    // 空白项，应丢弃
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Members) != 2 || cm.Members[0].Model != "glm-flash" || cm.Members[1].Model != "deepseek-free" {
		t.Errorf("成员规范化错误: %+v", cm.Members)
	}
	if cm.Status != model.ChannelEnabled {
		t.Errorf("默认状态应为启用: %v", cm.Status)
	}

	// 重名报错
	if _, err := svc.Create(ctx, &service.CustomModelInput{Name: "free-1M", Members: []model.ModelMember{{Model: "x"}}}); err == nil {
		t.Error("重名应报错")
	}
	// 空成员报错
	if _, err := svc.Create(ctx, &service.CustomModelInput{Name: "empty", Members: nil}); err == nil {
		t.Error("空成员应报错")
	}

	svc.Create(ctx, &service.CustomModelInput{Name: "other", Members: []model.ModelMember{{Model: "y"}}})
	disabled := model.ChannelDisabled
	up, err := svc.Update(ctx, cm.ID, &service.CustomModelInput{
		Name: "other2", Members: []model.ModelMember{{Model: "z", Priority: 3}}, Status: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if up.Name != "other2" || up.Status != model.ChannelDisabled || len(up.Members) != 1 || up.Members[0].Priority != 3 {
		t.Errorf("更新未生效: %+v", up)
	}
	// 改成已存在的名字应报错
	if _, err := svc.Update(ctx, cm.ID, &service.CustomModelInput{Name: "other", Members: []model.ModelMember{{Model: "z"}}}); err == nil {
		t.Error("更新为重名应报错")
	}

	if _, total, err := svc.List(ctx, pagination.Normalize(1, 20)); err != nil || total != 2 {
		t.Errorf("list: %v %d", err, total)
	}
	if err := svc.Delete(ctx, cm.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, cm.ID); err == nil {
		t.Error("删除后查询应报错")
	}
}
