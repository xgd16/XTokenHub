// Package service 业务逻辑层：渠道管理（含探测）、请求日志、统计。
package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"xtokenhub/internal/eventbus"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
)

// ChannelService 渠道业务。
type ChannelService struct {
	repo   repository.ChannelRepository
	prober *provider.Prober
	bus    *eventbus.Bus

	// 余额查询（见 balance.go）：client 为 nil 时所有渠道视为不支持。
	balMu     sync.Mutex
	balClient *provider.BalanceClient
	balKind   func(string) provider.BalanceProviderKind
	balCache  map[int64]balanceCacheEntry
}

// NewChannelService 构造渠道服务。
func NewChannelService(repo repository.ChannelRepository, prober *provider.Prober, bus *eventbus.Bus) *ChannelService {
	return &ChannelService{repo: repo, prober: prober, bus: bus, balKind: provider.InferBalanceProvider}
}

// CreateInput 创建/更新渠道入参。Provider 可省略：按 BaseURL 自动推断接口风格
// （域名/路径含 anthropic → Anthropic 风格，否则 OpenAI 兼容），显式指定时仅作覆盖。
type CreateInput struct {
	Name      string               `json:"name" binding:"required,max=128"`
	Provider  model.ProviderType   `json:"provider"`
	BaseURL   string               `json:"base_url" binding:"required,url,max=512"`
	APIKey    string               `json:"api_key" binding:"required,max=256"`
	Models    []string             `json:"models"`
	Protocols []model.Protocol     `json:"native_protocols"` // 可选：手工指定原生协议
	Priority  *int                 `json:"priority"`
	Weight    *int                 `json:"weight"`
	Status    *model.ChannelStatus `json:"status"`
	Remark    string               `json:"remark" binding:"max=512"`
}

// resolveProvider 补全/校验接口风格。
func (in *CreateInput) resolveProvider() error {
	if in.Provider == "" {
		in.Provider = provider.InferProviderType(in.BaseURL)
		return nil
	}
	if !in.Provider.Valid() {
		return errs.New(errs.CodeInvalidParams, "不支持的接口风格: "+string(in.Provider))
	}
	return nil
}

func (s *CreateInput) apply(ch *model.Channel) {
	ch.Name = s.Name
	ch.Provider = s.Provider
	ch.BaseURL = s.BaseURL
	ch.APIKey = s.APIKey
	ch.Models = joinModels(s.Models)
	ch.Remark = s.Remark
	if s.Priority != nil {
		ch.Priority = *s.Priority
	}
	if s.Weight != nil {
		ch.Weight = *s.Weight
	}
	if s.Status != nil {
		ch.Status = *s.Status
	}
	if len(s.Protocols) > 0 {
		ch.SetProtocols(s.Protocols)
	}
}

func joinModels(ms []string) string {
	out := ""
	for i, m := range ms {
		if i > 0 {
			out += ","
		}
		out += m
	}
	return out
}

// Create 创建渠道。
func (s *ChannelService) Create(ctx context.Context, in *CreateInput) (*model.Channel, error) {
	if err := in.resolveProvider(); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetByName(ctx, in.Name); err == nil {
		return nil, errs.New(errs.CodeInvalidParams, "渠道名已存在")
	}
	ch := &model.Channel{Status: model.ChannelEnabled, Priority: 100, Weight: 1}
	in.apply(ch)
	if err := s.repo.Create(ctx, ch); err != nil {
		return nil, err
	}
	return ch, nil
}

// Update 更新渠道。
func (s *ChannelService) Update(ctx context.Context, id int64, in *CreateInput) (*model.Channel, error) {
	if err := in.resolveProvider(); err != nil {
		return nil, err
	}
	ch, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if dup, err := s.repo.GetByName(ctx, in.Name); err == nil && dup.ID != id {
		return nil, errs.New(errs.CodeInvalidParams, "渠道名已存在")
	}
	in.apply(ch)
	if err := s.repo.Update(ctx, ch); err != nil {
		return nil, err
	}
	s.balInvalidate(ch.ID)
	return ch, nil
}

// Delete 删除渠道。
func (s *ChannelService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.balInvalidate(id)
	return nil
}

// Get 查询渠道。
func (s *ChannelService) Get(ctx context.Context, id int64) (*model.Channel, error) {
	return s.repo.Get(ctx, id)
}

// List 渠道列表。
func (s *ChannelService) List(ctx context.Context, p pagination.Params) ([]model.Channel, int64, error) {
	return s.repo.List(ctx, p)
}

// LookupModels 用给定凭据拉取上游模型列表（渠道可尚未创建，供前端建渠道时选择）。
// 子路径 BaseURL（如 DeepSeek 的 https://api.deepseek.com/anthropic）自身无列表接口时，降级尝试主域名。
func (s *ChannelService) LookupModels(ctx context.Context, providerType model.ProviderType, baseURL, apiKey string) ([]string, error) {
	return s.prober.FetchModelsFallback(ctx, baseURL, providerType, apiKey)
}

// Probe 触发渠道原生协议探测并更新库，返回报告。
// 探测模型只使用渠道最终保存的支持模型（列表第一项）；
// 用户留空（支持全部模型）时才自动发现：上游模型列表 -> 主域名兜底列表 -> 内置兜底，
// 避免用不存在的模型导致探测误判。
func (s *ChannelService) Probe(ctx context.Context, id int64, probeModel string) (*provider.ProbeReport, error) {
	ch, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	pm := probeModel
	if pm == "" {
		if list := ch.ModelList(); len(list) > 0 {
			pm = list[0]
		} else if ms, ferr := s.prober.FetchModels(ctx, ch.BaseURL, ch.Provider, ch.APIKey); ferr == nil && len(ms) > 0 {
			pm = ms[0]
		} else if ms, ferr := s.prober.FetchModelsFallback(ctx, ch.BaseURL, ch.Provider, ch.APIKey); ferr == nil && len(ms) > 0 {
			pm = ms[0]
		}
	}
	report := s.prober.Probe(ctx, ch.BaseURL, ch.APIKey, ch.Provider, pm)
	ch.SetProtocols(report.NativeProtocols)
	now := time.Now()
	ch.LastProbeAt = &now
	ch.ProbeResult = encodeProbeResult(report)
	if err := s.repo.Update(ctx, ch); err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(eventbus.EventChannelProbeResult, map[string]any{
			"channel_id": ch.ID, "channel_name": ch.Name, "report": report,
		})
	}
	logger.L("channel").Info("probe finished",
		"channel", ch.Name, "native", stringList(report.NativeProtocols))
	return report, nil
}

func encodeProbeResult(r *provider.ProbeReport) string {
	b, err := json.Marshal(r.Items)
	if err != nil {
		return ""
	}
	return string(b)
}

func stringList(ps []model.Protocol) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}

// LogService 请求日志业务。
type LogService struct {
	repo repository.RequestLogRepository
}

// NewLogService 构造日志服务。
func NewLogService(repo repository.RequestLogRepository) *LogService {
	return &LogService{repo: repo}
}

// ListInput 日志查询条件（HTTP 层）。
type ListInput struct {
	Protocol    string `form:"protocol"`
	ForwardMode string `form:"forward_mode"`
	ChannelID   int64  `form:"channel_id"`
	KeyID       int64  `form:"key_id"`
	Model       string `form:"model"`
	Stream      *bool  `form:"stream"`
	ErrorOnly   bool   `form:"error_only"`
	Hours       int    `form:"hours"` // 最近 N 小时
}

// List 查询日志。
func (s *LogService) List(ctx context.Context, in ListInput, p pagination.Params) ([]model.RequestLog, int64, error) {
	f := repository.LogFilter{
		Protocol:    model.Protocol(in.Protocol),
		ForwardMode: model.ForwardMode(in.ForwardMode),
		ChannelID:   in.ChannelID,
		KeyID:       in.KeyID,
		Model:       in.Model,
		Stream:      in.Stream,
		ErrorOnly:   in.ErrorOnly,
	}
	if in.Hours > 0 {
		since := time.Now().Add(-time.Duration(in.Hours) * time.Hour)
		f.StartTime = &since
	}
	return s.repo.List(ctx, f, p)
}

// StatsService 统计业务。
type StatsService struct {
	repo repository.RequestLogRepository
}

// NewStatsService 构造统计服务。
func NewStatsService(repo repository.RequestLogRepository) *StatsService {
	return &StatsService{repo: repo}
}

// Range 统计时间范围（小时）。
func rangeSince(hours int) time.Time {
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*90 {
		hours = 24 * 90
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour)
}

// Summary 汇总。
func (s *StatsService) Summary(ctx context.Context, hours int) (*repository.Summary, error) {
	return s.repo.Summary(ctx, rangeSince(hours))
}

// TrendByDay 按日趋势。
func (s *StatsService) TrendByDay(ctx context.Context, days int) ([]repository.TrendPoint, error) {
	if days <= 0 {
		days = 7
	}
	if days > 400 {
		days = 400
	}
	return s.repo.TrendByDay(ctx, time.Now().AddDate(0, 0, -days))
}

// TrendByBucket 按分钟/小时分桶趋势（bucketSec 为桶宽秒数）。
func (s *StatsService) TrendByBucket(ctx context.Context, hours int, bucketSec int64) ([]repository.TrendPoint, error) {
	return s.repo.TrendByBucket(ctx, rangeSince(hours), bucketSec)
}

// ByModel 按模型聚合。
func (s *StatsService) ByModel(ctx context.Context, hours int) ([]repository.GroupStat, error) {
	return s.repo.GroupByModel(ctx, rangeSince(hours))
}

// ByChannel 按渠道聚合。
func (s *StatsService) ByChannel(ctx context.Context, hours int) ([]repository.GroupStat, error) {
	return s.repo.GroupByChannel(ctx, rangeSince(hours))
}

// ByKey 按调用方密钥聚合。
func (s *StatsService) ByKey(ctx context.Context, hours int) ([]repository.GroupStat, error) {
	return s.repo.GroupByKey(ctx, rangeSince(hours))
}

// ---- 支持 since（Unix 秒）显式指定窗口起点：前端传本地零点即「当天」口径 ----

// sinceOrRange since 优先（>0 视为 Unix 秒），否则回退最近 hours 小时。
func sinceOrRange(sinceSec int64, hours int) time.Time {
	if sinceSec > 0 {
		return time.Unix(sinceSec, 0)
	}
	return rangeSince(hours)
}

// SummaryFlex 汇总（since 优先）。
func (s *StatsService) SummaryFlex(ctx context.Context, sinceSec int64, hours int) (*repository.Summary, error) {
	return s.repo.Summary(ctx, sinceOrRange(sinceSec, hours))
}

// ByModelFlex 按模型聚合（since 优先）。
func (s *StatsService) ByModelFlex(ctx context.Context, sinceSec int64, hours int) ([]repository.GroupStat, error) {
	return s.repo.GroupByModel(ctx, sinceOrRange(sinceSec, hours))
}

// TrendByDayModel 按日 × 模型聚合 token 用量。
func (s *StatsService) TrendByDayModel(ctx context.Context, days int) ([]repository.ModelDayPoint, error) {
	if days <= 0 {
		days = 7
	}
	if days > 400 {
		days = 400
	}
	return s.repo.TrendByDayModel(ctx, time.Now().AddDate(0, 0, -days))
}

// LifetimeStat 全历史累计统计（由按日聚合推导）。
type LifetimeStat struct {
	TotalTokens   int64  `json:"total_tokens"`
	PeakDayTokens int64  `json:"peak_day_tokens"`
	PeakDay       string `json:"peak_day"`
	MaxDurationMS int64  `json:"max_duration_ms"` // 单次请求最长耗时（成功请求）
	CurrentStreak int    `json:"current_streak"`  // 当前连续使用天数
	MaxStreak     int    `json:"max_streak"`      // 最长连续使用天数
	ActiveDays    int    `json:"active_days"`     // 有请求的天数
}

// Lifetime 全历史累计统计：总 token、峰值日、最长单次耗时、连续使用天数。
func (s *StatsService) Lifetime(ctx context.Context) (*LifetimeStat, error) {
	days, err := s.repo.DailyUsage(ctx, time.Time{})
	if err != nil {
		return nil, err
	}
	st := &LifetimeStat{}
	var streak, maxStreak int
	var prev time.Time
	for _, d := range days {
		st.TotalTokens += d.TotalTokens
		st.ActiveDays++
		if d.TotalTokens > st.PeakDayTokens {
			st.PeakDayTokens = d.TotalTokens
			st.PeakDay = d.Date
		}
		if d.MaxDurationMS > st.MaxDurationMS {
			st.MaxDurationMS = d.MaxDurationMS
		}
		cur, perr := time.ParseInLocation("2006-01-02", d.Date, time.Local)
		if perr != nil {
			continue
		}
		if !prev.IsZero() && cur.Equal(prev.AddDate(0, 0, 1)) {
			streak++
		} else {
			streak = 1
		}
		if streak > maxStreak {
			maxStreak = streak
		}
		prev = cur
	}
	st.MaxStreak = maxStreak
	// 当前连续：从今天往回数；今天还没有请求时从昨天延续（与 GitHub 连续提交同口径）
	if len(days) > 0 {
		n := time.Now()
		today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
		last, err := time.ParseInLocation("2006-01-02", days[len(days)-1].Date, time.Local)
		if err == nil && (last.Equal(today) || last.Equal(today.AddDate(0, 0, -1))) {
			st.CurrentStreak = streak
		}
	}
	return st, nil
}
