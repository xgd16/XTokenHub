package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

// stubClassifier 分类补全桩：返回预设 JSON，可拦截调用次数与提示词。
type stubClassifier struct {
	mu      sync.Mutex
	calls   int
	prompts []string
	fn      func(system, user string) (string, error)
}

func (s *stubClassifier) Classify(_ context.Context, ch *model.Channel, modelName, system, user string) (string, error) {
	s.mu.Lock()
	s.calls++
	s.prompts = append(s.prompts, user)
	s.mu.Unlock()
	if ch == nil || modelName == "" {
		return "", errors.New("渠道或模型为空")
	}
	return s.fn(system, user)
}

func (s *stubClassifier) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubClassifier) lastPrompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.prompts) == 0 {
		return ""
	}
	return s.prompts[len(s.prompts)-1]
}

// classifyEnv 分类任务装配：内存库 + 渠道 + 设置 + 桩 LLM。
type classifyEnv struct {
	svc        *service.MemoryClassifier
	memories   repository.MemoryRepository
	namespaces repository.MemoryNamespaceRepository
	settings   repository.MemorySettingsRepository
	channels   repository.ChannelRepository
	llm        *stubClassifier
}

func newClassifyEnv(t *testing.T) *classifyEnv {
	t.Helper()
	db := testutil.NewMemoryDB(t)
	env := &classifyEnv{
		memories:   repository.NewMemoryRepository(db),
		namespaces: repository.NewMemoryNamespaceRepository(db),
		settings:   repository.NewMemorySettingsRepository(db),
		channels:   repository.NewChannelRepository(db),
		llm:        &stubClassifier{},
	}
	ctx := context.Background()
	if err := env.settings.Save(ctx, &model.MemorySettings{ID: 1}); err != nil {
		t.Fatalf("播种设置行: %v", err)
	}
	if err := env.channels.Create(ctx, &model.Channel{
		Name: "main", Provider: model.ProviderOpenAICompatible,
		BaseURL: "https://api.example.com", APIKey: "sk-test",
		Status: model.ChannelEnabled,
	}); err != nil {
		t.Fatalf("创建渠道: %v", err)
	}
	env.svc = service.NewMemoryClassifier(env.memories, env.namespaces, env.settings, env.channels,
		env.llm, service.MemoryClassifyOptions{})
	return env
}

// seedUncategorized 写入 n 条未分类记忆（标题按序号）。
func (e *classifyEnv) seedUncategorized(t *testing.T, n int) []int64 {
	t.Helper()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		m := &model.Memory{
			Title: fmt.Sprintf("待分类 %d", i), Content: fmt.Sprintf("第 %d 条未分类记忆的正文", i),
			Kind: model.MemoryKindNote, Importance: 3, Status: model.MemoryStatusActive,
			Source: model.MemorySourceAPI, ContentHash: fmt.Sprintf("h-%d", i),
		}
		if err := e.memories.Create(context.Background(), m); err != nil {
			t.Fatalf("写入记忆: %v", err)
		}
		ids = append(ids, m.ID)
	}
	return ids
}

func (e *classifyEnv) configure(t *testing.T, channelID int64, modelName string) {
	t.Helper()
	if err := e.settings.UpdateConfig(context.Background(), &model.MemorySettings{
		ID: 1, ClassifierChannelID: channelID, ClassifierModel: modelName,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyRequiresConfig(t *testing.T) {
	env := newClassifyEnv(t)
	env.seedUncategorized(t, 2)

	st, err := env.svc.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != model.MemoryJobDone || !strings.Contains(st.Message, "未配置分类模型") {
		t.Fatalf("未配置时应提示配置: %+v", st)
	}
	if env.llm.callCount() != 0 {
		t.Fatal("未配置不应调用 LLM")
	}
}

func TestClassifyAssignAndCreateNamespaces(t *testing.T) {
	env := newClassifyEnv(t)
	ids := env.seedUncategorized(t, 3)
	env.configure(t, 1, "mimo-v2")

	// 两条归入新命名空间，一条跳过。
	env.llm.fn = func(system, user string) (string, error) {
		if !strings.Contains(system, "命名空间") {
			t.Errorf("system 提示词缺少任务说明: %s", system)
		}
		if !strings.Contains(user, "待分类") || !strings.Contains(user, fmt.Sprintf("id=%d", ids[0])) {
			t.Errorf("user 提示词缺少记忆清单: %s", user)
		}
		return "```json\n" + `{"assignments":[{"id":` + fmt.Sprint(ids[0]) + `,"namespace":"支付回调"},{"id":` + fmt.Sprint(ids[1]) + `,"namespace":"none"},{"id":9999,"namespace":"幽灵"}],` +
			`"new_namespaces":[{"name":"支付回调","description":"支付渠道对接与回调验签"},{"name":"部署运维","description":""}]}` + "\n```", nil
	}

	st, err := env.svc.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != model.MemoryJobDone || st.Total != 3 || st.Done != 3 || st.Assigned != 1 {
		t.Fatalf("任务统计: %+v", st)
	}
	// 新建命名空间：只算被归类实际引用的「支付回调」；「部署运维」无人归入 → 不建空壳。
	if st.NewNamespaces != 1 {
		t.Fatalf("新建命名空间数 = %d", st.NewNamespaces)
	}
	m, err := env.memories.Get(context.Background(), ids[0])
	if err != nil || m.Namespace != "支付回调" || m.NamespaceSource != model.NamespaceSourceLLM {
		t.Fatalf("归类结果: %v %+v", err, m)
	}
	// "none" 与批次外 id 不生效。
	if got, _ := env.memories.Get(context.Background(), ids[1]); got.Namespace != "" {
		t.Fatalf("none 不应归类: %+v", got)
	}
	if got, _ := env.memories.Get(context.Background(), ids[2]); got.Namespace != "" {
		t.Fatalf("跳过条目应保持未分类: %+v", got)
	}
	if ns, err := env.namespaces.GetByName(context.Background(), "支付回调"); err != nil || ns.CreatedBy != string(model.MemorySourceSystem) {
		t.Fatalf("命名空间注册行: %v %+v", err, ns)
	}
}

func TestClassifyBadOutputKeepsUnclassified(t *testing.T) {
	env := newClassifyEnv(t)
	ids := env.seedUncategorized(t, 1)
	env.configure(t, 1, "mimo-v2")
	env.llm.fn = func(string, string) (string, error) {
		return "抱歉，我无法输出 JSON", nil
	}

	st, err := env.svc.Run(context.Background())
	if err == nil {
		t.Fatal("解析失败应返回错误")
	}
	if st.Status != model.MemoryJobFailed || !strings.Contains(st.Message, "解析失败") {
		t.Fatalf("失败状态: %+v", st)
	}
	if got, _ := env.memories.Get(context.Background(), ids[0]); got.Namespace != "" {
		t.Fatalf("失败后不应改动记忆: %+v", got)
	}
	// 进度可查（双写者：配置列不受影响）。
	cfg, err := env.settings.Get(context.Background())
	if err != nil || cfg.ClassifierModel != "mimo-v2" {
		t.Fatalf("配置列被覆盖: %v %+v", err, cfg)
	}
}

func TestClassifyLLMErrorAndChannelState(t *testing.T) {
	env := newClassifyEnv(t)
	env.seedUncategorized(t, 1)
	env.configure(t, 1, "mimo-v2")
	env.llm.fn = func(string, string) (string, error) { return "", errors.New("上游 500") }

	st, err := env.svc.Run(context.Background())
	if err == nil || st.Status != model.MemoryJobFailed || !strings.Contains(st.Message, "分类调用失败") {
		t.Fatalf("LLM 失败应记 failed: %+v err=%v", st, err)
	}

	// 渠道停用。
	if err := env.channels.Update(context.Background(), &model.Channel{
		ID: 1, Name: "main", Provider: model.ProviderOpenAICompatible,
		BaseURL: "https://api.example.com", APIKey: "sk-test", Status: model.ChannelDisabled,
	}); err != nil {
		t.Fatal(err)
	}
	st, err = env.svc.Run(context.Background())
	if err == nil || st.Status != model.MemoryJobFailed || !strings.Contains(st.Message, "停用") {
		t.Fatalf("渠道停用应记 failed: %+v err=%v", st, err)
	}
}

func TestClassifyResistsReentry(t *testing.T) {
	env := newClassifyEnv(t)
	env.seedUncategorized(t, 1)
	env.configure(t, 1, "mimo-v2")

	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	env.llm.fn = func(string, string) (string, error) {
		once.Do(func() { close(started) })
		<-release
		return `{"assignments":[]}`, nil
	}

	done := make(chan error, 1)
	go func() {
		_, err := env.svc.Run(context.Background())
		done <- err
	}()
	<-started
	if _, err := env.svc.Run(context.Background()); err == nil || errs.From(err).Code != errs.CodeInvalidParams {
		t.Fatalf("并发触发应被拒绝: %v", err)
	}
	if _, err := env.svc.Start(context.Background()); err == nil {
		t.Fatal("并发 Start 应被拒绝")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClassifyResetStale(t *testing.T) {
	env := newClassifyEnv(t)
	ctx := context.Background()
	start := time.Now()
	if err := env.settings.UpdateJobStatus(ctx, repository.MemoryJobStatus{
		Status: model.MemoryJobRunning, Total: 5, Done: 2, StartedAt: &start,
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.ResetStale(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := env.svc.Status(ctx)
	if st.Status != model.MemoryJobFailed || !strings.Contains(st.Message, "中断") {
		t.Fatalf("重启后 running 应置 failed: %+v", st)
	}
	// 非 running 状态不动。
	if err := env.svc.ResetStale(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ = env.svc.Status(ctx)
	if !strings.Contains(st.Message, "中断") {
		t.Fatalf("done 状态不应被改写: %+v", st)
	}
}

func TestClassifyExistingNamespaceReuse(t *testing.T) {
	env := newClassifyEnv(t)
	ctx := context.Background()
	ids := env.seedUncategorized(t, 1)
	if _, err := env.namespaces.Ensure(ctx, "缓存策略", "缓存与命中率", "api"); err != nil {
		t.Fatal(err)
	}
	env.configure(t, 1, "mimo-v2")
	env.llm.fn = func(system, user string) (string, error) {
		if !strings.Contains(system, "缓存策略 - 缓存与命中率") {
			t.Errorf("已有命名空间未进提示词: %s", system)
		}
		return fmt.Sprintf(`{"assignments":[{"id":%d,"namespace":"缓存策略"}]}`, ids[0]), nil
	}
	st, err := env.svc.Run(ctx)
	if err != nil || st.Assigned != 1 || st.NewNamespaces != 0 {
		t.Fatalf("复用命名空间: %+v err=%v", st, err)
	}
}
