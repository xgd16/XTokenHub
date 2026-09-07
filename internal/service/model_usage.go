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
}

// NewModelService 构造模型服务。
func NewModelService(logs repository.RequestLogRepository, channels repository.ChannelRepository, customs repository.CustomModelRepository) *ModelService {
	return &ModelService{logs: logs, channels: channels, customs: customs}
}

// Usage 全部模型的多时间窗（1h/24h/7d/30d）使用量。
// 模型目录 = 启用渠道配置的模型 ∪ 启用自定义模型组的成员 ∪ 近 30 日日志出现过的模型；
// 请求日志记录的是发往上游的真实模型（分组路由即成员模型），组名不参与用量归集；
// 目录中尚无调用的模型各窗口为零值。按 30 日 token 降序、名称升序。
func (s *ModelService) Usage(ctx context.Context) ([]repository.ModelUsageStat, error) {
	stats, err := s.logs.ModelUsage(ctx, time.Now())
	if err != nil {
		return nil, err
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
