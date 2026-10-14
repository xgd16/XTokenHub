package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/pkg/textseg"
	"xtokenhub/internal/repository"
)

// 分类任务参数与提示词边界。
const (
	// classifyDefaultBatch 单次调用送分类的记忆条数。
	classifyDefaultBatch = 20
	// classifyDefaultMaxBatches 单轮任务批次数上限（防失控写入）。
	classifyDefaultMaxBatches = 200
	// classifyPageSize 取未分类记忆的分页大小。
	classifyPageSize = 200
	// classifySummaryRunes 单条记忆送入提示词的正文摘要长度（runes）。
	classifySummaryRunes = 200
)

// ClassifierLLM 分类补全调用（由 internal/provider 的 ChannelClassifier 实现；测试注入桩）。
type ClassifierLLM interface {
	// Classify 对渠道 ch 的指定模型发起一次非流式补全，返回文本。
	Classify(ctx context.Context, ch *model.Channel, modelName, system, user string) (string, error)
}

// MemoryClassifyOptions 分类任务运行参数。
type MemoryClassifyOptions struct {
	// BatchSize 单次调用送分类的记忆条数（0 = 默认 20）。
	BatchSize int
	// MaxBatches 单轮任务批次数上限（0 = 默认 200）。
	MaxBatches int
}

// MemoryClassifier 一键 LLM 分类：把未分类记忆按主题归入命名空间（无向量、无词典）。
// 显式归属优先——本任务只处理 namespace 为空的记忆，且只改命名空间字段。
type MemoryClassifier struct {
	memories   repository.MemoryRepository
	namespaces repository.MemoryNamespaceRepository
	settings   repository.MemorySettingsRepository
	channels   repository.ChannelRepository
	llm        ClassifierLLM
	opts       MemoryClassifyOptions
	now        func() time.Time

	mu sync.Mutex // 防重入（TryLock）
}

// NewMemoryClassifier 构造分类任务。
func NewMemoryClassifier(
	memories repository.MemoryRepository,
	namespaces repository.MemoryNamespaceRepository,
	settings repository.MemorySettingsRepository,
	channels repository.ChannelRepository,
	llm ClassifierLLM,
	opts MemoryClassifyOptions,
) *MemoryClassifier {
	if opts.BatchSize <= 0 {
		opts.BatchSize = classifyDefaultBatch
	}
	if opts.MaxBatches <= 0 {
		opts.MaxBatches = classifyDefaultMaxBatches
	}
	return &MemoryClassifier{
		memories: memories, namespaces: namespaces, settings: settings, channels: channels,
		llm: llm, opts: opts, now: time.Now,
	}
}

// SetClock 注入时钟（测试用）。
func (s *MemoryClassifier) SetClock(now func() time.Time) { s.now = now }

// classifyJobStatus 最近一次任务状态（读 memory_settings 任务列）。
func (s *MemoryClassifier) classifyJobStatus(ctx context.Context) (*repository.MemoryJobStatus, error) {
	st, err := s.settings.Get(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &repository.MemoryJobStatus{}, nil
		}
		return nil, errs.Wrap(errs.CodeInternal, "读取分类任务状态失败", err)
	}
	return &repository.MemoryJobStatus{
		Status: st.JobStatus, Total: st.JobTotal, Done: st.JobDone,
		Assigned: st.JobAssigned, NewNamespaces: st.JobNewNamespaces,
		Message: st.JobMessage, StartedAt: st.JobStartedAt, FinishedAt: st.JobFinishedAt,
	}, nil
}

// Status 当前任务状态。
func (s *MemoryClassifier) Status(ctx context.Context) (*repository.MemoryJobStatus, error) {
	return s.classifyJobStatus(ctx)
}

// ResetStale 服务启动时调用：上轮任务若因重启中断（状态卡 running）则标记为失败，
// 避免按钮永远显示「进行中」。
func (s *MemoryClassifier) ResetStale(ctx context.Context) error {
	st, err := s.classifyJobStatus(ctx)
	if err != nil {
		return err
	}
	if st.Status != model.MemoryJobRunning {
		return nil
	}
	now := s.now()
	return s.settings.UpdateJobStatus(ctx, repository.MemoryJobStatus{
		Status: model.MemoryJobFailed, Total: st.Total, Done: st.Done,
		Assigned: st.Assigned, NewNamespaces: st.NewNamespaces,
		Message:   "服务重启导致任务中断，可重新触发",
		StartedAt: st.StartedAt, FinishedAt: &now,
	})
}

// Start 异步启动一轮分类；已在运行则报错。
func (s *MemoryClassifier) Start(ctx context.Context) (*repository.MemoryJobStatus, error) {
	if !s.mu.TryLock() {
		return nil, errs.New(errs.CodeInvalidParams, "分类任务正在进行中，请稍候")
	}
	start := s.now()
	st := &repository.MemoryJobStatus{Status: model.MemoryJobRunning, StartedAt: &start}
	if err := s.settings.UpdateJobStatus(ctx, *st); err != nil {
		s.mu.Unlock()
		return nil, errs.Wrap(errs.CodeInternal, "写入任务状态失败", err)
	}
	go func() {
		defer s.mu.Unlock()
		// 后台续跑不绑定请求上下文；单次补全调用自带超时。
		_, _ = s.run(context.Background())
	}()
	return st, nil
}

// Run 同步执行一轮分类（测试与脚本用）。
func (s *MemoryClassifier) Run(ctx context.Context) (*repository.MemoryJobStatus, error) {
	if !s.mu.TryLock() {
		return nil, errs.New(errs.CodeInvalidParams, "分类任务正在进行中，请稍候")
	}
	defer s.mu.Unlock()
	return s.run(ctx)
}

// run 一轮分类：取全部未分类 → 分批送 LLM → 只改命名空间归属 → 全程写回进度。
func (s *MemoryClassifier) run(ctx context.Context) (*repository.MemoryJobStatus, error) {
	start := s.now()
	finish := func(st repository.MemoryJobStatus, msg string, runErr error) (*repository.MemoryJobStatus, error) {
		now := s.now()
		st.FinishedAt = &now
		st.Message = textseg.Clip(msg, 300)
		if runErr != nil {
			st.Status = model.MemoryJobFailed
		} else {
			st.Status = model.MemoryJobDone
		}
		if err := s.settings.UpdateJobStatus(ctx, st); err != nil && runErr == nil {
			runErr = errs.Wrap(errs.CodeInternal, "写入任务状态失败", err)
		}
		return &st, runErr
	}
	job := repository.MemoryJobStatus{Status: model.MemoryJobRunning, StartedAt: &start}

	// 分类模型配置（控制台 → 记忆库 → 分类设置）。
	cfg, err := s.settings.Get(ctx)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return finish(job, "读取分类设置失败", errs.Wrap(errs.CodeInternal, "读取分类设置失败", err))
	}
	if cfg == nil || cfg.ClassifierChannelID <= 0 || cfg.ClassifierModel == "" {
		return finish(job, "未配置分类模型：请在控制台「记忆库 → 分类设置」选择渠道与模型", nil)
	}
	ch, err := s.channels.Get(ctx, cfg.ClassifierChannelID)
	if err != nil {
		return finish(job, "分类渠道不可用", errs.Wrap(errs.CodeInvalidParams, "分类渠道不可用", err))
	}
	if ch.Status != model.ChannelEnabled {
		return finish(job, "分类渠道已停用", errs.New(errs.CodeInvalidParams, "分类渠道已停用"))
	}

	// 快照未分类记忆（分页收集，处理阶段不再翻页，避免「LLM 漏项」反复入批）。
	f := repository.MemoryFilter{Uncategorized: true, Status: string(model.MemoryStatusActive)}
	pending := make([]model.Memory, 0, classifyPageSize)
	for offset := 0; ; offset += classifyPageSize {
		items, total, err := s.memories.List(ctx, f, "updated", offset, classifyPageSize)
		if err != nil {
			return finish(job, "读取未分类记忆失败", errs.Wrap(errs.CodeInternal, "读取未分类记忆失败", err))
		}
		pending = append(pending, items...)
		if len(items) == 0 || int64(len(pending)) >= total {
			break
		}
	}
	job.Total = len(pending)
	if len(pending) == 0 {
		return finish(job, "没有未分类记忆", nil)
	}
	if err := s.settings.UpdateJobStatus(ctx, job); err != nil {
		return finish(job, "写入任务状态失败", err)
	}

	for batch := 0; batch < s.opts.MaxBatches && len(pending) > 0; batch++ {
		n := s.opts.BatchSize
		if n > len(pending) {
			n = len(pending)
		}
		items := pending[:n]
		pending = pending[n:]

		// 每批都取最新命名空间：本 run 前几批新建的命名空间也能被后续批次复用。
		namespaces, err := s.namespaces.List(ctx)
		if err != nil {
			return finish(job, "读取命名空间失败", errs.Wrap(errs.CodeInternal, "读取命名空间失败", err))
		}

		raw, err := s.llm.Classify(ctx, ch, cfg.ClassifierModel, classifySystemPrompt(namespaces), classifyUserPrompt(items))
		if err != nil {
			return finish(job, "分类调用失败: "+err.Error(), errs.Wrap(errs.CodeUpstream, "分类调用失败", err))
		}
		out, err := parseClassifyOutput(raw)
		if err != nil {
			return finish(job, "分类结果解析失败: "+err.Error(), errs.Wrap(errs.CodeInternal, "分类结果解析失败", err))
		}
		assigned, created, applyErr := s.applyClassify(ctx, out, items)
		job.Done += n
		job.Assigned += assigned
		job.NewNamespaces += created
		if applyErr != nil {
			return finish(job, "分类结果落库失败: "+applyErr.Error(), applyErr)
		}
		// 批间写回进度，控制台可实时看到。
		if err := s.settings.UpdateJobStatus(ctx, job); err != nil {
			logger.L("memory-classify").Warn("写回分类进度失败", logger.Err(err))
		}
	}

	return finish(job,
		fmt.Sprintf("已处理 %d 条，归入命名空间 %d 条，新建命名空间 %d 个", job.Done, job.Assigned, job.NewNamespaces), nil)
}

// classifyAssignment 单条记忆的归属结论。
type classifyAssignment struct {
	ID        int64  `json:"id"`
	Namespace string `json:"namespace"`
}

// classifyNewNamespace 新建命名空间建议。
type classifyNewNamespace struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// classifyOutput 分类模型的结构化输出。
type classifyOutput struct {
	Assignments   []classifyAssignment   `json:"assignments"`
	NewNamespaces []classifyNewNamespace `json:"new_namespaces"`
}

// classifySystemPrompt 任务指令：命名空间是唯一归类单位，优先复用既有命名空间。
func classifySystemPrompt(namespaces []model.MemoryNamespace) string {
	var b strings.Builder
	b.WriteString("你是记忆库的整理助手。任务：为一批待分类记忆选择合适的「命名空间」。\n")
	b.WriteString("命名空间是记忆库唯一的归类单位（可理解为主题/板块），命名空间名应简短凝练（2~8 个字），如「支付回调」「部署运维」「缓存策略」。\n\n")
	if len(namespaces) > 0 {
		b.WriteString("已有命名空间（优先复用；名称 - 描述）：\n")
		for _, ns := range namespaces {
			b.WriteString("- " + ns.Name)
			if ns.Description != "" {
				b.WriteString(" - " + ns.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	} else {
		b.WriteString("当前还没有命名空间，全部由你新建。\n\n")
	}
	b.WriteString("规则：\n")
	b.WriteString("1. 优先归入已有命名空间；确实都不合适才新建（new_namespaces 里给名称与一句话描述）。\n")
	b.WriteString("2. 每条记忆只归一个命名空间；拿不准就归入最接近的一个，不要为单条记忆新建专属命名空间。\n")
	b.WriteString("3. 没把握的条目可以不给 assignments（它会保持未分类）。\n")
	b.WriteString("4. 只输出 JSON，不要任何解释。JSON 结构：\n")
	b.WriteString(`{"assignments":[{"id":1,"namespace":"支付回调"}],"new_namespaces":[{"name":"部署运维","description":"发布、环境与运维相关"}]}`)
	b.WriteString("\n")
	return b.String()
}

// classifyUserPrompt 待分类记忆清单（id | 标题 | 摘要）。
func classifyUserPrompt(items []model.Memory) string {
	var b strings.Builder
	b.WriteString("待分类记忆：\n")
	for i, m := range items {
		summary := textseg.Clip(strings.ReplaceAll(m.Content, "\n", " "), classifySummaryRunes)
		fmt.Fprintf(&b, "%d. id=%d | %s | %s\n", i+1, m.ID, m.Title, summary)
	}
	return b.String()
}

// parseClassifyOutput 解析模型输出（容忍代码围栏与前后杂文，只取 JSON 对象）。
func parseClassifyOutput(raw string) (*classifyOutput, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("模型返回为空")
	}
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	// 常见代码围栏残留。
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	var out classifyOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// applyClassify 落库：把 assignments 里的记忆归入命名空间（source=llm）。
// new_namespaces 的建议只有被 assignments 实际引用时才创建（避免空壳命名空间），
// 描述随首条归类写入。只改命名空间字段，不动正文。返回归类数与新建命名空间数。
func (s *MemoryClassifier) applyClassify(ctx context.Context, out *classifyOutput, items []model.Memory) (int, int, error) {
	batch := make(map[int64]model.Memory, len(items))
	for _, m := range items {
		batch[m.ID] = m
	}
	// 建议描述：命名空间被引用时随建随写。
	wantDesc := make(map[string]string, len(out.NewNamespaces))
	for _, nn := range out.NewNamespaces {
		name := strings.TrimSpace(nn.Name)
		if validClassifyNamespace(name) {
			wantDesc[name] = textseg.Clip(strings.TrimSpace(nn.Description), 512)
		}
	}

	assigned, created := 0, 0
	createdNames := make(map[string]bool, len(wantDesc))
	for _, a := range out.Assignments {
		name := strings.TrimSpace(a.Namespace)
		if !validClassifyNamespace(name) {
			continue
		}
		m, ok := batch[a.ID]
		if !ok {
			continue // 不属于本批次的 id：忽略
		}
		isNew, err := s.namespaces.Ensure(ctx, name, wantDesc[name], string(model.MemorySourceSystem))
		if err != nil {
			return assigned, created, errs.Wrap(errs.CodeInternal, "创建命名空间失败", err)
		}
		if isNew && !createdNames[name] {
			created++
			createdNames[name] = true
		}
		m.Namespace = name
		m.NamespaceSource = model.NamespaceSourceLLM
		if err := s.memories.Update(ctx, &m); err != nil {
			return assigned, created, errs.Wrap(errs.CodeInternal, "归类落库失败", err)
		}
		assigned++
	}
	return assigned, created, nil
}

// validClassifyNamespace 分类产出的命名空间名合法性（"none" 为未分类过滤保留值）。
func validClassifyNamespace(name string) bool {
	if name == "" || name == "none" {
		return false
	}
	return len([]rune(name)) <= model.MemoryNamespaceMaxRunes
}
