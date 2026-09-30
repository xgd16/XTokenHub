package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
)

// dailyStatsRepo DailyStatsRepository 的 GORM 实现。
type dailyStatsRepo struct {
	db *gorm.DB
}

// NewDailyStatsRepository 构造日级聚合仓储。
func NewDailyStatsRepository(db *gorm.DB) DailyStatsRepository {
	return &dailyStatsRepo{db: db}
}

// usageDayInsert 单日聚合写入（占位符：day, date, now, start, end）。
// 口径与 log_repo 的实时聚合一致：错误数含全部失败请求；token/缓存按全量求和
// （与 DailyUsage/TrendByDay 一致，含失败请求；Summary 排除失败请求且不走聚合表）；
// max_duration_ms 为成功请求最大耗时。
const usageDayInsert = `INSERT INTO daily_usage_stats
	(day, date, requests, error_requests, prompt_tokens, completion_tokens, cached_tokens, cost_usd, max_duration_ms, updated_at)
	SELECT ?, ?, COUNT(*),
		COALESCE(SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(prompt_tokens), 0),
		COALESCE(SUM(completion_tokens), 0),
		COALESCE(SUM(cached_tokens), 0),
		COALESCE(SUM(cost_usd), 0),
		COALESCE(MAX(CASE WHEN error = '' THEN duration_ms ELSE 0 END), 0),
		?
	FROM request_logs
	WHERE created_at >= ? AND created_at < ?`

// modelDayInsert 单日 × 模型聚合写入（占位符同上）。
const modelDayInsert = `INSERT INTO daily_model_stats
	(day, date, model, requests, prompt_tokens, completion_tokens, cached_tokens, cost_usd, updated_at)
	SELECT ?, ?, model, COUNT(*),
		COALESCE(SUM(prompt_tokens), 0),
		COALESCE(SUM(completion_tokens), 0),
		COALESCE(SUM(cached_tokens), 0),
		COALESCE(SUM(cost_usd), 0),
		?
	FROM request_logs
	WHERE created_at >= ? AND created_at < ?
	GROUP BY model`

// usageBackfillSQL 全量回填日级聚合：一次 GROUP BY 扫描现存全部明细（占位符：offset, offset, now, offset）。
// date(桶号*86400,'unixepoch') 即该本地日（与 DayBucketDate 同口径）。
const usageBackfillSQL = `INSERT INTO daily_usage_stats
	(day, date, requests, error_requests, prompt_tokens, completion_tokens, cached_tokens, cost_usd, max_duration_ms, updated_at)
	SELECT ` + dayBucketSQL + `,
		date(` + dayBucketSQL + ` * 86400, 'unixepoch'),
		COUNT(*),
		COALESCE(SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(prompt_tokens), 0),
		COALESCE(SUM(completion_tokens), 0),
		COALESCE(SUM(cached_tokens), 0),
		COALESCE(SUM(cost_usd), 0),
		COALESCE(MAX(CASE WHEN error = '' THEN duration_ms ELSE 0 END), 0),
		?
	FROM request_logs
	GROUP BY ` + dayBucketSQL

// modelBackfillSQL 全量回填日 × 模型聚合（占位符同上）。
const modelBackfillSQL = `INSERT INTO daily_model_stats
	(day, date, model, requests, prompt_tokens, completion_tokens, cached_tokens, cost_usd, updated_at)
	SELECT ` + dayBucketSQL + `,
		date(` + dayBucketSQL + ` * 86400, 'unixepoch'),
		model, COUNT(*),
		COALESCE(SUM(prompt_tokens), 0),
		COALESCE(SUM(completion_tokens), 0),
		COALESCE(SUM(cached_tokens), 0),
		COALESCE(SUM(cost_usd), 0),
		?
	FROM request_logs
	GROUP BY ` + dayBucketSQL + `, model`

// RebuildDay 重建单个本地日的聚合（两表同日先删后插，幂等）。
// 源日已无明细（被保留期清理）时不做任何改动：聚合行一旦生成，删除明细不影响累计，
// 因此本方法可以放心地对任何历史日调用。
func (r *dailyStatsRepo) RebuildDay(ctx context.Context, day int64) error {
	start, end := DayRange(day)
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.RequestLog{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	date := DayBucketDate(day)
	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("day = ?", day).Delete(&model.DailyUsageStat{}).Error; err != nil {
			return err
		}
		if err := tx.Where("day = ?", day).Delete(&model.DailyModelStat{}).Error; err != nil {
			return err
		}
		if err := tx.Exec(usageDayInsert, day, date, now, start, end).Error; err != nil {
			return err
		}
		return tx.Exec(modelDayInsert, day, date, now, start, end).Error
	})
}

// BackfillAll 全量回填：清空两张聚合表后按现存明细整体重建，返回聚合出的天数。
// 一次性成本（两次全表 GROUP BY），仅在首次初始化与手动全量重建时调用。
func (r *dailyStatsRepo) BackfillAll(ctx context.Context) (int64, error) {
	offset := localDayOffset()
	now := time.Now()
	var days int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.DailyUsageStat{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.DailyModelStat{}).Error; err != nil {
			return err
		}
		res := tx.Exec(usageBackfillSQL, offset, offset, now, offset)
		if res.Error != nil {
			return res.Error
		}
		days = res.RowsAffected
		return tx.Exec(modelBackfillSQL, offset, offset, now, offset).Error
	})
	return days, err
}

// SumDays 汇总 [fromDay, toDay] 的日级聚合；fromDay=0 表示不限起点。
// max_duration_ms 是「跨日取最大」而不是求和（单日已是当日最大耗时）。
func (r *dailyStatsRepo) SumDays(ctx context.Context, fromDay, toDay int64) (*model.DailyUsageStat, error) {
	var s model.DailyUsageStat
	q := r.db.WithContext(ctx).Model(&model.DailyUsageStat{})
	if fromDay > 0 {
		q = q.Where("day >= ?", fromDay)
	}
	err := q.Where("day <= ?", toDay).Select(
		"COALESCE(SUM(requests),0) AS requests, " +
			"COALESCE(SUM(error_requests),0) AS error_requests, " +
			"COALESCE(SUM(prompt_tokens),0) AS prompt_tokens, " +
			"COALESCE(SUM(completion_tokens),0) AS completion_tokens, " +
			"COALESCE(SUM(cached_tokens),0) AS cached_tokens, " +
			"COALESCE(SUM(cost_usd),0) AS cost_usd, " +
			"COALESCE(MAX(max_duration_ms),0) AS max_duration_ms",
	).Scan(&s).Error
	return &s, err
}

// ListDays 列出 [fromDay, toDay] 的日级聚合（按日升序）；fromDay=0 表示不限起点。
func (r *dailyStatsRepo) ListDays(ctx context.Context, fromDay, toDay int64) ([]model.DailyUsageStat, error) {
	var rows []model.DailyUsageStat
	q := r.db.WithContext(ctx).Model(&model.DailyUsageStat{})
	if fromDay > 0 {
		q = q.Where("day >= ?", fromDay)
	}
	err := q.Where("day <= ?", toDay).Order("day ASC").Find(&rows).Error
	return rows, err
}

// ListModelDays 列出 [fromDay, toDay] 的日 × 模型聚合（按日、模型升序）。
func (r *dailyStatsRepo) ListModelDays(ctx context.Context, fromDay, toDay int64) ([]model.DailyModelStat, error) {
	var rows []model.DailyModelStat
	err := r.db.WithContext(ctx).Model(&model.DailyModelStat{}).
		Where("day >= ? AND day <= ?", fromDay, toDay).
		Order("day ASC, model ASC").Find(&rows).Error
	return rows, err
}

// MinMaxDay 已聚合的最小/最大日桶号（表为空返回 0, 0），供设置页展示覆盖范围。
func (r *dailyStatsRepo) MinMaxDay(ctx context.Context) (minDay, maxDay int64, err error) {
	var row struct {
		MinDay int64
		MaxDay int64
	}
	err = r.db.WithContext(ctx).Model(&model.DailyUsageStat{}).
		Select("COALESCE(MIN(day),0) AS min_day, COALESCE(MAX(day),0) AS max_day").
		Scan(&row).Error
	return row.MinDay, row.MaxDay, err
}
