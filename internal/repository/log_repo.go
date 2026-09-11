package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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

// DeleteBefore 删除 created_at 早于 cutoff 的日志，单批最多 limit 行（子查询 LIMIT 分批，
// 依赖 idx_request_logs_created_at），避免单条大 DELETE 长时间占用 SQLite 单写连接。
func (r *logRepo) DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	res := r.db.WithContext(ctx).
		Where("id IN (SELECT id FROM request_logs WHERE created_at < ? LIMIT ?)", cutoff, limit).
		Delete(&model.RequestLog{})
	return res.RowsAffected, res.Error
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
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
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
		Cost    float64
	}{}
	err := r.statWindow(ctx, since).Select(
		"COUNT(*) AS total, " +
			"SUM(CASE WHEN error = '' THEN 1 ELSE 0 END) AS success, " +
			"COALESCE(SUM(CASE WHEN error = '' THEN prompt_tokens ELSE 0 END),0) AS prompt, " +
			"COALESCE(SUM(CASE WHEN error = '' THEN completion_tokens ELSE 0 END),0) AS compl, " +
			"COALESCE(SUM(CASE WHEN error = '' THEN cached_tokens ELSE 0 END),0) AS cached, " +
			"COALESCE(AVG(CASE WHEN error = '' THEN duration_ms END),0) AS avg_ms, " +
			"COALESCE(SUM(cost_usd),0) AS cost, " +
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
	s.CostUSD = row.Cost
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
			"COALESCE(SUM(cost_usd),0) AS cost_usd, "+
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
			"COALESCE(SUM(cost_usd),0) AS cost_usd, " +
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
	Cost        float64
}) []GroupStat {
	out := make([]GroupStat, 0, len(rows))
	for _, r := range rows {
		gs := GroupStat{
			Name:        r.Name,
			Requests:    r.Requests,
			TotalTokens: r.TotalTokens,
			CachedToken: r.CachedToken,
			AvgMS:       r.AvgMS,
			CostUSD:     r.Cost,
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
		Cost        float64
	}
	err := r.statWindow(ctx, since).Select(
		"model AS name, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, " +
			"COALESCE(SUM(cached_tokens),0) AS cached_token, " +
			"COALESCE(SUM(cost_usd),0) AS cost, " +
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
		Cost        float64
	}
	err := r.statWindow(ctx, since).Select(
		"COALESCE(channel_name,'unknown') AS name, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, " +
			"COALESCE(SUM(cached_tokens),0) AS cached_token, " +
			"COALESCE(SUM(cost_usd),0) AS cost, " +
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
		Cost        float64
	}
	err := r.statWindow(ctx, since).Select(
		"CASE WHEN key_name = '' THEN '(匿名)' ELSE key_name END AS name, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens, " +
			"COALESCE(SUM(cached_tokens),0) AS cached_token, " +
			"COALESCE(SUM(cost_usd),0) AS cost, " +
			"COALESCE(AVG(duration_ms),0) AS avg_ms",
	).Group("key_id, key_name").Order("requests DESC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return groupScan(rows), nil
}

// CostSince 时间窗 [since, until) 内的费用合计与计入请求数（until 为零值表示到当前）。
func (r *logRepo) CostSince(ctx context.Context, since, until time.Time) (float64, int64, error) {
	var row struct {
		Cost     float64
		Requests int64
	}
	q := r.db.WithContext(ctx).Model(&model.RequestLog{}).Where("created_at >= ?", since)
	if !until.IsZero() {
		q = q.Where("created_at < ?", until)
	}
	if err := q.Select("COALESCE(SUM(cost_usd),0) AS cost, COUNT(*) AS requests").Scan(&row).Error; err != nil {
		return 0, 0, err
	}
	return row.Cost, row.Requests, nil
}

// CostByDay 按日聚合费用与请求数（本地时区日期键）。
func (r *logRepo) CostByDay(ctx context.Context, since time.Time) ([]DayCost, error) {
	var rows []DayCost
	err := r.statWindow(ctx, since).Select(
		"DATE(created_at, 'localtime') AS date, COUNT(*) AS requests, " +
			"COALESCE(SUM(cost_usd),0) AS cost_usd",
	).Group("DATE(created_at, 'localtime')").Order("date ASC").Scan(&rows).Error
	if rows == nil {
		rows = []DayCost{}
	}
	return rows, err
}

// ModelsWithUsage 时间窗内有用量的模型及其用量，按总 token 倒序，最多 limit 条。
// 不在此处判定「未定价」：别名匹配规则在 provider 层，SQL 只能精确同名匹配会漏判。
func (r *logRepo) ModelsWithUsage(ctx context.Context, since time.Time, limit int) ([]ModelUsage, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	var rows []ModelUsage
	err := r.statWindow(ctx, since).Select(
		"model AS model, COUNT(*) AS requests, " +
			"COALESCE(SUM(prompt_tokens + completion_tokens),0) AS total_tokens",
	).Group("model").Order("total_tokens DESC").Limit(limit).Scan(&rows).Error
	if rows == nil {
		rows = []ModelUsage{}
	}
	return rows, err
}

// RecomputeBatch 按游标取一批待重算费用的日志（id 升序）。
func (r *logRepo) RecomputeBatch(ctx context.Context, from, to time.Time, afterID int64, limit int, onlyMissing bool) ([]model.RequestLog, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	q := r.db.WithContext(ctx).Model(&model.RequestLog{}).
		Where("created_at >= ?", from).
		Where("id > ?", afterID).
		Order("id ASC").Limit(limit)
	if !to.IsZero() {
		q = q.Where("created_at < ?", to)
	}
	if onlyMissing {
		// cost_usd 可能为 NULL：旧库升级时 AutoMigrate 只加列、不回填，历史行是 NULL，
		// 而 SQL 里 NULL <= 0 求值为 NULL（非 TRUE），只写 <= 0 会漏掉全部历史行。
		q = q.Where("cost_usd IS NULL OR cost_usd <= 0")
	}
	var rows []model.RequestLog
	err := q.Find(&rows).Error
	return rows, err
}

// UpdateCost 回写单条日志的费用、计价口径与计价时段（只更新三列，避免覆盖并发写入的其他字段）。
func (r *logRepo) UpdateCost(ctx context.Context, id int64, costUSD float64, style model.UsageStyle, period model.PricePeriod) error {
	return r.db.WithContext(ctx).Model(&model.RequestLog{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"cost_usd":     costUSD,
			"usage_style":  style,
			"price_period": period,
		}).Error
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

// sessionKeyExpr 会话分组键：有 session_id 按会话归并，否则按自身主键独立成组。
// 与前端 groupBySession 的分组语义一致（sess:<id> / req:<id>）。
const sessionKeyExpr = "CASE WHEN session_id <> '' THEN 'sess:' || session_id ELSE 'req:' || id END"

// LiveSessions 最近活跃会话聚合：先按会话键取最近活跃的 N 组，再拉这些组的全部行
// 在内存中汇总——统计覆盖全量历史，不因前端只保留 200 行而残缺。
func (r *logRepo) LiveSessions(ctx context.Context, in LiveSessionsInput) ([]LiveSession, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	// 1) 取最近活跃的会话键（按组内最新请求时间倒序）。
	// MAX(created_at) 仅用于排序，不扫描成 time.Time（聚合结果由驱动返回字符串）。
	var keys []struct {
		Key string `gorm:"column:skey"`
	}
	err := r.db.WithContext(ctx).Model(&model.RequestLog{}).
		Select(sessionKeyExpr + " AS skey, MAX(created_at) AS last_at").
		Group("skey").Order("last_at DESC").Limit(limit).Scan(&keys).Error
	if err != nil {
		return nil, fmt.Errorf("live sessions keys: %w", err)
	}
	if len(keys) == 0 {
		return []LiveSession{}, nil
	}

	// 2) 拉取这些会话的全部请求行（含历史），按会话键 + 时间倒序
	skeys := make([]string, 0, len(keys))
	for _, k := range keys {
		skeys = append(skeys, k.Key)
	}
	var rows []model.RequestLog
	err = r.db.WithContext(ctx).Model(&model.RequestLog{}).
		Where(sessionKeyExpr+" IN ?", skeys).
		Order("created_at DESC, id DESC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("live sessions rows: %w", err)
	}

	// 3) 内存聚合，保持 keys 的活跃顺序；时间边界由实际行计算。
	// rows 已按 created_at DESC 排序，每个键首次出现的行即组内最新行。
	byKey := make(map[string]*LiveSession, len(keys))
	for i := range keys {
		byKey[keys[i].Key] = &LiveSession{
			Key:       keys[i].Key,
			Models:    []string{},
			Channels:  []string{},
			Protocols: []string{},
			Modes:     []string{},
		}
	}
	latest := make(map[string]*model.RequestLog, len(keys))
	for i := range rows {
		row := &rows[i]
		key := "req:" + strconv.FormatInt(row.ID, 10)
		if row.SessionID != "" {
			key = "sess:" + row.SessionID
		}
		g := byKey[key]
		if g == nil {
			continue
		}
		if latest[key] == nil {
			latest[key] = row
		}
		if g.SessionID == "" && row.SessionID != "" {
			g.SessionID = row.SessionID
		}
		g.Requests++
		g.PromptTokens += row.PromptTokens
		g.CompletionTokens += row.CompletionTokens
		// 与 executor.finish 口径一致：total 缺失时回退为 prompt+completion
		g.TotalTokens += rowTotalTokens(row)
		g.CachedTokens += row.CachedTokens
		g.TotalMS += row.DurationMS
		if row.Error != "" {
			g.Errors++
		}
		if row.CreatedAt.Before(g.FirstAt) || g.FirstAt.IsZero() {
			g.FirstAt = row.CreatedAt
		}
		if row.CreatedAt.After(g.LastAt) {
			g.LastAt = row.CreatedAt
		}
		if g.UserAgent == "" && row.UserAgent != "" {
			g.UserAgent = row.UserAgent
		}
		if g.KeyName == "" && row.KeyName != "" {
			g.KeyName = row.KeyName
		}
		g.Models = appendUnique(g.Models, row.Model)
		g.Channels = appendUnique(g.Channels, row.ChannelName)
		g.Protocols = appendUnique(g.Protocols, string(row.Protocol))
		g.Modes = appendUnique(g.Modes, string(row.ForwardMode))
	}

	out := make([]LiveSession, 0, len(keys))
	for i := range keys {
		g := byKey[keys[i].Key]
		if g == nil {
			continue
		}
		// 散行（单次请求）直接带上整行，前端无需再查即可展示状态/明细/请求头
		if g.Requests == 1 {
			g.LastRequest = latest[keys[i].Key]
		}
		out = append(out, *g)
	}
	return out, nil
}

// rowTotalTokens 单行总 token：落库值优先，缺失时回退为 prompt+completion。
func rowTotalTokens(r *model.RequestLog) int64 {
	if r.TotalTokens > 0 {
		return r.TotalTokens
	}
	return r.PromptTokens + r.CompletionTokens
}

// appendUnique 去重追加（保持首次出现顺序）。
func appendUnique(list []string, v string) []string {	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
