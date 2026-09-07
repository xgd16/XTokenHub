package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
)

// logRepo RequestLogRepository 的 GORM 实现。
type logRepo struct {
	db *gorm.DB
}

// NewRequestLogRepository 构造请求日志仓储。
func NewRequestLogRepository(db *gorm.DB) RequestLogRepository {
	return &logRepo{db: db}
}

func (r *logRepo) Create(ctx context.Context, l *model.RequestLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

func (r *logRepo) Get(ctx context.Context, id int64) (*model.RequestLog, error) {
	var l model.RequestLog
	if err := r.db.WithContext(ctx).First(&l, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "日志不存在")
		}
		return nil, err
	}
	return &l, nil
}

func (r *logRepo) baseQuery(ctx context.Context, f LogFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.RequestLog{})
	if f.Protocol != "" {
		q = q.Where("protocol = ?", f.Protocol)
	}
	if f.ForwardMode != "" {
		q = q.Where("forward_mode = ?", f.ForwardMode)
	}
	if f.ChannelID > 0 {
		q = q.Where("channel_id = ?", f.ChannelID)
	}
	if f.KeyID > 0 {
		q = q.Where("key_id = ?", f.KeyID)
	}
	if f.Model != "" {
		q = q.Where("model = ?", f.Model)
	}
	if f.Stream != nil {
		q = q.Where("stream = ?", *f.Stream)
	}
	if f.StartTime != nil {
		q = q.Where("created_at >= ?", *f.StartTime)
	}
	if f.EndTime != nil {
		q = q.Where("created_at < ?", *f.EndTime)
	}
	if f.ErrorOnly {
		q = q.Where("error <> ''")
	}
	return q
}

func (r *logRepo) List(ctx context.Context, f LogFilter, p pagination.Params) ([]model.RequestLog, int64, error) {
	var (
		items []model.RequestLog
		total int64
	)
	q := r.baseQuery(ctx, f)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count logs: %w", err)
	}
	err := q.Order("id DESC").Offset(p.Offset).Limit(p.Limit).Find(&items).Error
	// 空 slice 统一返回 []，避免 JSON 序列化成 null 破坏前端数组约定
	if items == nil {
		items = []model.RequestLog{}
	}
	return items, total, err
}

// statWindow 统计公共过滤：时间窗内（错误也计入 error 维度，但 usage 聚合只算成功请求）。
func (r *logRepo) statWindow(ctx context.Context, since time.Time) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.RequestLog{}).Where("created_at >= ?", since)
}

func (r *logRepo) Summary(ctx context.Context, since time.Time) (*Summary, error) {
	var s Summary
	row := struct {
		Total   int64
		Success int64
		Prompt  int64
		Compl   int64
		Cached  int64
		AvgMS   float64
		Native  int64
	}{}
	err := r.statWindow(ctx, since).Select(
		"COUNT(*) AS total, " +
			"SUM(CASE WHEN error = '' THEN 1 ELSE 0 END) AS success, " +
			"COALESCE(SUM(CASE WHEN error = '' THEN prompt_tokens ELSE 0 END),0) AS prompt, " +
			"COALESCE(SUM(CASE WHEN error = '' THEN completion_tokens ELSE 0 END),0) AS compl, " +
			"COALESCE(SUM(CASE WHEN error = '' THEN cached_tokens ELSE 0 END),0) AS cached, " +
			"COALESCE(AVG(CASE WHEN error = '' THEN duration_ms END),0) AS avg_ms, " +
			"SUM(CASE WHEN forward_mode = 'native_passthrough' THEN 1 ELSE 0 END) AS native",
	).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	s.TotalRequests = row.Total
	s.SuccessReqs = row.Success
	s.ErrorReqs = row.Total - row.Success
	s.PromptTokens = row.Prompt
	s.CompletionTok = row.Compl
	s.TotalTokens = row.Prompt + row.Compl
	s.CachedTokens = row.Cached
	if row.Prompt > 0 {
		s.CacheHitRate = float64(row.Cached) / float64(row.Prompt)
	}
	s.AvgDurationMS = row.AvgMS
	if row.Total > 0 {
		s.NativeRatio = float64(row.Native) / float64(row.Total)
	}
	return &s, nil
}

// TrendByBucket 按固定桶宽（秒）聚合的趋势，桶起点为 epoch 对齐的 Unix 秒。
// Date 字段不填充，前端按 Ts 换算本地时间展示。
func (r *logRepo) TrendByBucket(ctx context.Context, since time.Time, bucketSec int64) ([]TrendPoint, error) {
	if bucketSec <= 0 {
		bucketSec = 3600
	}
	var points []TrendPoint
	expr := fmt.Sprintf(
		"CAST(strftime('%%s', created_at) AS INTEGER)/%d*%d AS ts, COUNT(*) AS requests, "+
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, "+
			"SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END) AS error_requests",
		bucketSec, bucketSec)
	err := r.statWindow(ctx, since).Select(expr).Group("ts").Order("ts ASC").Scan(&points).Error
	if points == nil {
		points = []TrendPoint{}
	}
	return points, err
}

func (r *logRepo) TrendByDay(ctx context.Context, since time.Time) ([]TrendPoint, error) {
	var points []TrendPoint
	err := r.statWindow(ctx, since).Select(
		"DATE(created_at, 'localtime') AS date, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, " +
			"SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END) AS error_requests",
	).Group("DATE(created_at, 'localtime')").Order("date ASC").Scan(&points).Error
	if points == nil {
		points = []TrendPoint{}
	}
	return points, err
}

func groupScan(rows []struct {
	Name        string
	Requests    int64
	TotalTokens int64
	CachedToken int64
	AvgMS       float64
}) []GroupStat {
	out := make([]GroupStat, 0, len(rows))
	for _, r := range rows {
		gs := GroupStat{
			Name:        r.Name,
			Requests:    r.Requests,
			TotalTokens: r.TotalTokens,
			CachedToken: r.CachedToken,
			AvgMS:       r.AvgMS,
		}
		if gs.TotalTokens > 0 {
			gs.CacheRate = float64(r.CachedToken) / float64(r.TotalTokens)
		}
		out = append(out, gs)
	}
	return out
}

func (r *logRepo) GroupByModel(ctx context.Context, since time.Time) ([]GroupStat, error) {
	var rows []struct {
		Name        string
		Requests    int64
		TotalTokens int64
		CachedToken int64
		AvgMS       float64
	}
	err := r.statWindow(ctx, since).Select(
		"model AS name, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, " +
			"COALESCE(SUM(cached_tokens),0) AS cached_token, " +
			"COALESCE(AVG(duration_ms),0) AS avg_ms",
	).Group("model").Order("requests DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return groupScan(rows), nil
}

// ModelUsage 按模型聚合 1h/24h/7d/30d 多时间窗使用量。
// 一次扫描近 30 日数据，更近的窗口用条件求和得出，避免多次全表聚合。
func (r *logRepo) ModelUsage(ctx context.Context, now time.Time) ([]ModelUsageStat, error) {
	var rows []struct {
		Model string `gorm:"column:model"`
		R1h   int64  `gorm:"column:r1h"`
		T1h   int64  `gorm:"column:t1h"`
		C1h   int64  `gorm:"column:c1h"`
		R24h  int64  `gorm:"column:r24h"`
		T24h  int64  `gorm:"column:t24h"`
		C24h  int64  `gorm:"column:c24h"`
		R7d   int64  `gorm:"column:r7d"`
		T7d   int64  `gorm:"column:t7d"`
		C7d   int64  `gorm:"column:c7d"`
		R30d  int64  `gorm:"column:r30d"`
		T30d  int64  `gorm:"column:t30d"`
		C30d  int64  `gorm:"column:c30d"`
	}
	t1h := now.Add(-time.Hour)
	t24h := now.Add(-24 * time.Hour)
	t7d := now.AddDate(0, 0, -7)
	err := r.db.WithContext(ctx).Model(&model.RequestLog{}).
		Where("created_at >= ?", now.AddDate(0, 0, -30)).
		Select(
			"model AS model, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END),0) AS r1h, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN prompt_tokens + completion_tokens ELSE 0 END),0) AS t1h, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN cached_tokens ELSE 0 END),0) AS c1h, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END),0) AS r24h, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN prompt_tokens + completion_tokens ELSE 0 END),0) AS t24h, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN cached_tokens ELSE 0 END),0) AS c24h, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END),0) AS r7d, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN prompt_tokens + completion_tokens ELSE 0 END),0) AS t7d, "+
				"COALESCE(SUM(CASE WHEN created_at >= ? THEN cached_tokens ELSE 0 END),0) AS c7d, "+
				"COUNT(*) AS r30d, "+
				"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS t30d, "+
				"COALESCE(SUM(cached_tokens),0) AS c30d",
			t1h, t1h, t1h, t24h, t24h, t24h, t7d, t7d, t7d,
		).Group("model").Order("t30d DESC, model ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ModelUsageStat, 0, len(rows))
	for _, row := range rows {
		out = append(out, ModelUsageStat{
			Model: row.Model,
			W1h:   ModelWindow{Requests: row.R1h, TotalTokens: row.T1h, CachedToken: row.C1h},
			W24h:  ModelWindow{Requests: row.R24h, TotalTokens: row.T24h, CachedToken: row.C24h},
			W7d:   ModelWindow{Requests: row.R7d, TotalTokens: row.T7d, CachedToken: row.C7d},
			W30d:  ModelWindow{Requests: row.R30d, TotalTokens: row.T30d, CachedToken: row.C30d},
		})
	}
	return out, nil
}

func (r *logRepo) GroupByChannel(ctx context.Context, since time.Time) ([]GroupStat, error) {
	var rows []struct {
		Name        string
		Requests    int64
		TotalTokens int64
		CachedToken int64
		AvgMS       float64
	}
	err := r.statWindow(ctx, since).Select(
		"COALESCE(channel_name,'unknown') AS name, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, " +
			"COALESCE(SUM(cached_tokens),0) AS cached_token, " +
			"COALESCE(AVG(duration_ms),0) AS avg_ms",
	).Group("channel_name").Order("requests DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return groupScan(rows), nil
}

func (r *logRepo) GroupByKey(ctx context.Context, since time.Time) ([]GroupStat, error) {
	var rows []struct {
		Name        string
		Requests    int64
		TotalTokens int64
		CachedToken int64
		AvgMS       float64
	}
	err := r.statWindow(ctx, since).Select(
		"CASE WHEN key_name = '' THEN '(匿名)' ELSE key_name END AS name, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, " +
			"COALESCE(SUM(cached_tokens),0) AS cached_token, " +
			"COALESCE(AVG(duration_ms),0) AS avg_ms",
	).Group("key_id, key_name").Order("requests DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return groupScan(rows), nil
}

// DailyUsage 按日聚合请求数 / token / 成功请求最大耗时。
func (r *logRepo) DailyUsage(ctx context.Context, since time.Time) ([]DayUsage, error) {
	var rows []DayUsage
	q := r.db.WithContext(ctx).Model(&model.RequestLog{})
	if !since.IsZero() {
		q = q.Where("created_at >= ?", since)
	}
	err := q.Select(
		"DATE(created_at, 'localtime') AS date, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, " +
			"COALESCE(MAX(CASE WHEN error = '' THEN duration_ms ELSE 0 END),0) AS max_duration_ms",
	).Group("DATE(created_at, 'localtime')").Order("date ASC").Scan(&rows).Error
	if rows == nil {
		rows = []DayUsage{}
	}
	return rows, err
}

// TrendByDayModel 按日 × 模型聚合 token 用量（含错误请求，与按日趋势口径一致）。
func (r *logRepo) TrendByDayModel(ctx context.Context, since time.Time) ([]ModelDayPoint, error) {
	var rows []ModelDayPoint
	err := r.statWindow(ctx, since).Select(
		"DATE(created_at, 'localtime') AS date, model, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens",
	).Group("DATE(created_at, 'localtime'), model").Order("date ASC").Scan(&rows).Error
	if rows == nil {
		rows = []ModelDayPoint{}
	}
	return rows, err
}
