package service

import (
	"context"
	"sort"
	"time"

	"xtokenhub/internal/repository"
)

// ModelService 模型目录与用量业务：合并渠道/自定义模型目录与请求日志的多时间窗用量。
type ModelService struct {
	logs     repository.RequestLogRepository
	channels repository.ChannelRepository
	customs  repository.CustomModelRepository
	daily    repository.DailyStatsRepository // 注入后 7d/30d 窗读日×模型聚合表
}

// NewModelService 构造模型服务。
func NewModelService(logs repository.RequestLogRepository, channels repository.ChannelRepository, customs repository.CustomModelRepository) *ModelService {
	return &ModelService{logs: logs, channels: channels, customs: customs}
}

// SetDailyStats 注入日级聚合仓储：注入后 w7d/w30d 改读聚合表（完整日）+ 当天实时，
// 明细扫描窗口从 30 天缩到 24 小时；不注入则维持全明细实时聚合（测试/降级路径）。
func (s *ModelService) SetDailyStats(daily repository.DailyStatsRepository) {
	s.daily = daily
}

// Usage 全部模型的多时间窗（1h/24h/7d/30d）使用量。
// 模型目录 = 启用渠道配置的模型 ∪ 启用自定义模型组的成员 ∪ 近 30 日日志出现过的模型；
// 请求日志记录的是发往上游的真实模型（分组路由即成员模型），组名不参与用量归集；
// 目录中尚无调用的模型各窗口为零值。按 30 日 token 降序、名称升序。
func (s *ModelService) Usage(ctx context.Context) ([]repository.ModelUsageStat, error) {
	var stats []repository.ModelUsageStat
	if s.daily != nil {
		merged, err := s.usageHybrid(ctx, time.Now())
		if err != nil {
			return nil, err
		}
		stats = merged
	} else {
		live, err := s.logs.ModelUsage(ctx, time.Now())
		if err != nil {
			return nil, err
		}
		stats = live
	}
	byName := make(map[string]*repository.ModelUsageStat, len(stats))
	for i := range stats {
		byName[stats[i].Model] = &stats[i]
	}
	ensure := func(name string) {
		if name == "" {
			return
		}
		if _, ok := byName[name]; !ok {
			byName[name] = &repository.ModelUsageStat{Model: name}
		}
	}
	chs, err := s.channels.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	for i := range chs {
		for _, m := range chs[i].ModelList() {
			ensure(m)
		}
	}
	if s.customs != nil {
		groups, gerr := s.customs.ListEnabled(ctx)
		if gerr != nil {
			return nil, gerr
		}
		for i := range groups {
			for _, m := range groups[i].MemberList() {
				ensure(m.Model)
			}
		}
	}
	out := make([]repository.ModelUsageStat, 0, len(byName))
	for _, v := range byName {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].W30d.TotalTokens != out[j].W30d.TotalTokens {
			return out[i].W30d.TotalTokens > out[j].W30d.TotalTokens
		}
		return out[i].Model < out[j].Model
	})
	return out, nil
}

// usageHybrid 混合口径的多时间窗用量：1h/24h/当天走近 24h 明细实时聚合，
// 7d/30d = 日×模型聚合表的完整日 + 当天实时部分。日历日口径（含今天）。
func (s *ModelService) usageHybrid(ctx context.Context, now time.Time) ([]repository.ModelUsageStat, error) {
	recent, err := s.logs.ModelRecent(ctx, now)
	if err != nil {
		return nil, err
	}
	today := repository.DayBucketOf(now)
	days, err := s.daily.ListModelDays(ctx, today-29, today-1)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*repository.ModelUsageStat, len(recent)+len(days))
	for i := range recent {
		r := &recent[i]
		byName[r.Model] = &repository.ModelUsageStat{Model: r.Model, W1h: r.W1h, W24h: r.W24h}
	}
	for _, d := range days {
		w := repository.ModelWindow{
			Requests:    d.Requests,
			TotalTokens: d.PromptTokens + d.CompletionTokens,
			CachedToken: d.CachedTokens,
		}
		m, ok := byName[d.Model]
		if !ok {
			m = &repository.ModelUsageStat{Model: d.Model}
			byName[d.Model] = m
		}
		m.W30d.Requests += w.Requests
		m.W30d.TotalTokens += w.TotalTokens
		m.W30d.CachedToken += w.CachedToken
		if d.Day >= today-6 {
			m.W7d.Requests += w.Requests
			m.W7d.TotalTokens += w.TotalTokens
			m.W7d.CachedToken += w.CachedToken
		}
	}
	// 当天实时部分并入 7d/30d（聚合表只含完整日）
	for i := range recent {
		m, ok := byName[recent[i].Model]
		if !ok {
			continue
		}
		m.W7d.Requests += recent[i].Today.Requests
		m.W7d.TotalTokens += recent[i].Today.TotalTokens
		m.W7d.CachedToken += recent[i].Today.CachedToken
		m.W30d.Requests += recent[i].Today.Requests
		m.W30d.TotalTokens += recent[i].Today.TotalTokens
		m.W30d.CachedToken += recent[i].Today.CachedToken
	}
	out := make([]repository.ModelUsageStat, 0, len(byName))
	for _, v := range byName {
		out = append(out, *v)
	}
	return out, nil
}
