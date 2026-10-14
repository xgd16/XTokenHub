// Package repository 数据访问层：面向接口定义，便于 service 层 mock 与测试。
package repository

import (
	"context"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/pagination"
)

// ChannelRepository 渠道数据访问接口。
type ChannelRepository interface {
	Create(ctx context.Context, ch *model.Channel) error
	Update(ctx context.Context, ch *model.Channel) error
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*model.Channel, error)
	GetByName(ctx context.Context, name string) (*model.Channel, error)
	List(ctx context.Context, p pagination.Params) ([]model.Channel, int64, error)
	ListAll(ctx context.Context) ([]model.Channel, error)
	ListEnabled(ctx context.Context) ([]model.Channel, error)
}

// CustomModelRepository 自定义模型组数据访问接口。
type CustomModelRepository interface {
	Create(ctx context.Context, cm *model.CustomModel) error
	Update(ctx context.Context, cm *model.CustomModel) error
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*model.CustomModel, error)
	GetByName(ctx context.Context, name string) (*model.CustomModel, error)
	List(ctx context.Context, p pagination.Params) ([]model.CustomModel, int64, error)
	ListEnabled(ctx context.Context) ([]model.CustomModel, error)
}

// RequestLogRepository 请求日志数据访问接口。
type RequestLogRepository interface {
	Create(ctx context.Context, log *model.RequestLog) error
	Get(ctx context.Context, id int64) (*model.RequestLog, error)
	List(ctx context.Context, f LogFilter, p pagination.Params) ([]model.RequestLog, int64, error)
	// Stats 聚合统计接口
	Summary(ctx context.Context, since time.Time) (*Summary, error)
	TrendByDay(ctx context.Context, since time.Time) ([]TrendPoint, error)
	TrendByBucket(ctx context.Context, since time.Time, bucketSec int64) ([]TrendPoint, error)
	GroupByModel(ctx context.Context, since time.Time) ([]GroupStat, error)
	GroupByChannel(ctx context.Context, since time.Time) ([]GroupStat, error)
	GroupByKey(ctx context.Context, since time.Time) ([]GroupStat, error)
	// ModelUsage 按模型聚合多时间窗（1h/24h/7d/30d）使用量，now 为窗口基准时刻。
	ModelUsage(ctx context.Context, now time.Time) ([]ModelUsageStat, error)
	// ModelRecent 近窗（1h/24h/当天零点起）按模型聚合：一次只扫近 24h 明细，
	// 7d/30d 窗由日 × 模型聚合表提供，service 层合并（见 ModelService.Usage）。
	ModelRecent(ctx context.Context, now time.Time) ([]ModelRecent, error)
	// TrimHeadersBefore 置空 created_at 早于 cutoff 的行的 request_headers，单批 limit 行，
	// 返回实际置空行数。请求头单行可达 4KB，是日志表体积大头，超期置空显著省空间。
	TrimHeadersBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)
	// DailyUsage 按日聚合使用量（含成功请求最大耗时），供全历史累计统计与热力图。
	DailyUsage(ctx context.Context, since time.Time) ([]DayUsage, error)
	// TrendByDayModel 按日 × 模型聚合 token 用量，供多模型趋势线。
	TrendByDayModel(ctx context.Context, since time.Time) ([]ModelDayPoint, error)
	// LiveSessions 最近活跃会话的聚合视图：会话统计覆盖全量历史（不受分页/前端窗口截断），
	// 无 session_id 的散行各自成组；按最后活跃时间倒序，最多 limit 组。
	LiveSessions(ctx context.Context, in LiveSessionsInput) ([]LiveSession, error)
	// DeleteBefore 分批删除 created_at 早于 cutoff 的日志，单批最多 limit 行，返回实际删除行数。
	// 供保留期清理任务使用；需反复调用直至返回数小于 limit。
	DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)

	// CostSince 时间窗内的费用合计（USD）与计入的请求数，供花费预测使用。
	CostSince(ctx context.Context, since, until time.Time) (float64, int64, error)
	// CostByDay 按日聚合费用，供预测的日均基准（run-rate）使用。
	CostByDay(ctx context.Context, since time.Time) ([]DayCost, error)
	// ModelsWithUsage 时间窗内有用量的模型及其用量，按总 token 倒序，最多 limit 条。
	// 是否「未定价」由 service 层用与计价热路径相同的别名匹配判定：SQL 只能精确同名
	// 匹配，而价格表键常带厂商前缀（如 zai/glm-5.3-flash），在 SQL 里过滤会误报。
	ModelsWithUsage(ctx context.Context, since time.Time, limit int) ([]ModelUsage, error)
	// RecomputeBatch 取一批待重算费用的日志（id 升序，afterID 为游标）；onlyMissing 时只取 cost_usd <= 0 的行。
	RecomputeBatch(ctx context.Context, from, to time.Time, afterID int64, limit int, onlyMissing bool) ([]model.RequestLog, error)
	// UpdateCost 回写单条日志的费用、计价口径与计价时段。
	UpdateCost(ctx context.Context, id int64, costUSD float64, style model.UsageStyle, period model.PricePeriod) error
}

// LogFilter 日志筛选条件，零值表示不过滤。
type LogFilter struct {
	Protocol    model.Protocol
	ForwardMode model.ForwardMode
	ChannelID   int64
	KeyID       int64
	Model       string
	SessionID   string
	Stream      *bool
	StartTime   *time.Time
	EndTime     *time.Time
	ErrorOnly   bool
}

// LiveSessionsInput 实时会话聚合查询条件。
type LiveSessionsInput struct {
	Limit int // 返回组数上限，<=0 取 20，最大 200
}

// LiveSession 会话聚合视图：同一 session_id 的全部请求合并为一行；无 session_id 的散行各自成组。
type LiveSession struct {
	Key              string    `json:"key"`
	SessionID        string    `json:"session_id"`
	Requests         int64     `json:"requests"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	CachedTokens     int64     `json:"cached_tokens"`
	TotalMS          int64     `json:"total_ms"`
	Errors           int64     `json:"errors"`
	FirstAt          time.Time `json:"first_at"`
	LastAt           time.Time `json:"last_at"`
	UserAgent        string    `json:"user_agent"`
	KeyName          string    `json:"key_name"`
	Models           []string  `json:"models"`
	Channels         []string  `json:"channels"`
	Protocols        []string  `json:"protocols"`
	Modes            []string  `json:"modes"` // 转发模式去重（native_passthrough / converted）
	// LastRequest 组内最新请求的完整行，仅散行（无 session_id，count=1）返回：
	// 供前端展示状态码 / 耗时明细 / 请求头，无需再额外查询。
	LastRequest *model.RequestLog `json:"last_request,omitempty"`
}

// Summary 汇总统计。
type Summary struct {
	TotalRequests int64   `json:"total_requests"`
	SuccessReqs   int64   `json:"success_requests"`
	ErrorReqs     int64   `json:"error_requests"`
	PromptTokens  int64   `json:"prompt_tokens"`
	CompletionTok int64   `json:"completion_tokens"`
	TotalTokens   int64   `json:"total_tokens"`
	CachedTokens  int64   `json:"cached_tokens"`
	CacheHitRate  float64 `json:"cache_hit_rate"`
	AvgDurationMS float64 `json:"avg_duration_ms"`
	NativeRatio   float64 `json:"native_ratio"` // 原生透传占比
	CostUSD       float64 `json:"cost_usd"`     // 窗口内费用合计（USD）
}

// TrendPoint 趋势点。按日查询时 Date 为日期键；分桶查询时 Ts 为桶起点 Unix 秒。
type TrendPoint struct {
	Date        string  `json:"date"`
	Ts          int64   `json:"ts,omitempty"`
	Requests    int64   `json:"requests"`
	TotalTokens int64   `json:"total_tokens"`
	ErrorReqs   int64   `json:"error_requests"`
	CostUSD     float64 `json:"cost_usd"`
}

// GroupStat 按维度聚合。
type GroupStat struct {
	Name        string  `json:"name"`
	Requests    int64   `json:"requests"`
	TotalTokens int64   `json:"total_tokens"`
	CachedToken int64   `json:"cached_tokens"`
	CacheRate   float64 `json:"cache_rate"`
	AvgMS       float64 `json:"avg_ms"`
	CostUSD     float64 `json:"cost_usd"`
}

// DayCost 单日费用聚合。
type DayCost struct {
	Date     string  `json:"date"` // YYYY-MM-DD（本地时区）
	Requests int64   `json:"requests"`
	CostUSD  float64 `json:"cost_usd"`
}

// ModelUsage 时间窗内有用量的模型及其用量。
type ModelUsage struct {
	Model       string `json:"model"`
	Requests    int64  `json:"requests"`
	TotalTokens int64  `json:"total_tokens"`
}

// ModelPriceRepository 模型价格数据访问接口。
type ModelPriceRepository interface {
	Create(ctx context.Context, p *model.ModelPrice) error
	Update(ctx context.Context, p *model.ModelPrice) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*model.ModelPrice, error)
	// GetByModel 按模型名查价格，不存在返回 gorm.ErrRecordNotFound。
	GetByModel(ctx context.Context, name string) (*model.ModelPrice, error)
	List(ctx context.Context) ([]model.ModelPrice, error)
	// ListPaged 分页查询，q 为模型名模糊匹配（空 = 不限），usedOnly 只返回有用量的模型，
	// peakOnly 只返回配置了峰谷时段价的模型（仪表盘峰谷模块用，避免大表翻页漏行）。
	ListPaged(ctx context.Context, q string, usedOnly, peakOnly bool, offset, limit int) ([]model.ModelPrice, int64, error)
	// ReplaceSynced 整体替换同步来源的价格（事务内先删 source=synced 再批量写入），
	// 手工配置（source=manual）的行始终保留。返回写入行数与跳过的同名手工行数。
	ReplaceSynced(ctx context.Context, prices []model.ModelPrice) (int, int, error)
	// Count 价格表总行数。
	Count(ctx context.Context) (int64, error)
}

// HolidayDateRepository 节假日年历数据访问接口。
type HolidayDateRepository interface {
	// ReplaceYear 事务内整体替换某年标注（先删该年再批量写入），返回写入行数。
	ReplaceYear(ctx context.Context, year int, rows []model.HolidayDate) (int, error)
	// List 全部标注，按日期升序（内存缓存加载用）。
	List(ctx context.Context) ([]model.HolidayDate, error)
	// ListYear 某年标注，按日期升序。
	ListYear(ctx context.Context, year int) ([]model.HolidayDate, error)
	// Count 标注总行数。
	Count(ctx context.Context) (int64, error)
}

// BillingSettingsRepository 计费展示设置数据访问接口。
type BillingSettingsRepository interface {
	// Get 读取单行设置，不存在返回 gorm.ErrRecordNotFound。
	Get(ctx context.Context) (*model.BillingSettings, error)
	Save(ctx context.Context, s *model.BillingSettings) error
}

// ReasoningRewriteRepository 思维档位改写规则数据访问接口。
type ReasoningRewriteRepository interface {
	Create(ctx context.Context, rule *model.ReasoningRewrite) error
	Update(ctx context.Context, rule *model.ReasoningRewrite) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*model.ReasoningRewrite, error)
	// GetByModel 按上游模型名精确查询，不存在返回 gorm.ErrRecordNotFound。
	GetByModel(ctx context.Context, name string) (*model.ReasoningRewrite, error)
	List(ctx context.Context) ([]model.ReasoningRewrite, error)
	// ListPaged 分页查询，q 为模型名模糊匹配（空 = 不限）。
	ListPaged(ctx context.Context, q string, offset, limit int) ([]model.ReasoningRewrite, int64, error)
}

// DailyStatsRepository 日级聚合数据访问接口（daily_usage_stats / daily_model_stats）。
// 聚合行按本地日一行，封存后不再变动：保留期清理明细不影响累计统计。
type DailyStatsRepository interface {
	// RebuildDay 重建单个本地日的聚合（两表同日先删后插，幂等）；
	// 源日已无明细时不做任何改动（保护已封存的累计数据）。
	RebuildDay(ctx context.Context, day int64) error
	// BackfillAll 全量回填：清空聚合表后按现存明细整体重建，返回聚合出的天数。
	BackfillAll(ctx context.Context) (int64, error)
	// SumDays 汇总 [fromDay, toDay] 的日级聚合；fromDay=0 表示不限起点。
	SumDays(ctx context.Context, fromDay, toDay int64) (*model.DailyUsageStat, error)
	// ListDays 列出 [fromDay, toDay] 的日级聚合（按日升序）；fromDay=0 表示不限起点。
	ListDays(ctx context.Context, fromDay, toDay int64) ([]model.DailyUsageStat, error)
	// ListModelDays 列出 [fromDay, toDay] 的日 × 模型聚合（按日、模型升序）。
	ListModelDays(ctx context.Context, fromDay, toDay int64) ([]model.DailyModelStat, error)
	// MinMaxDay 已聚合的最小/最大日桶号（表为空返回 0, 0）。
	MinMaxDay(ctx context.Context) (minDay, maxDay int64, err error)
}

// DataSettingsRepository 数据管理设置数据访问接口（单行表，ID 固定为 1）。
type DataSettingsRepository interface {
	// Get 读取单行设置，不存在返回 gorm.ErrRecordNotFound。
	Get(ctx context.Context) (*model.DataSettings, error)
	// Save 全量保存（首次播种用）。
	Save(ctx context.Context, s *model.DataSettings) error
	// UpdateConfig 只更新清理配置列，避免覆盖后台任务写回的运行状态。
	UpdateConfig(ctx context.Context, s *model.DataSettings) error
	// UpdateAggStatus 聚合任务写回水位线与最近执行时间。
	UpdateAggStatus(ctx context.Context, watermarkDay int64, at time.Time) error
	// UpdateCleanupStatus 清理任务写回最近一次执行结果。
	UpdateCleanupStatus(ctx context.Context, at time.Time, day int64, deleted, trimmed, durationMS int64, lastErr string) error
}

// MimoSettingsRepository MiMo 网页余额凭证数据访问接口（单行表，ID 固定为 1）。
type MimoSettingsRepository interface {
	// Get 读取单行设置，不存在返回 gorm.ErrRecordNotFound。
	Get(ctx context.Context) (*model.MimoSettings, error)
	// Save 全量保存（首次播种用）。
	Save(ctx context.Context, s *model.MimoSettings) error
	// UpdateCookie 只更新凭证列。
	UpdateCookie(ctx context.Context, cookie string) error
}

// ModelWindow 模型在单个时间窗内的使用量。
type ModelWindow struct {
	Requests    int64 `json:"requests"`
	TotalTokens int64 `json:"total_tokens"`
	CachedToken int64 `json:"cached_tokens"`
}

// ModelUsageStat 单个模型的多时间窗使用量。
type ModelUsageStat struct {
	Model string      `json:"model"`
	W1h   ModelWindow `json:"w_1h"`
	W24h  ModelWindow `json:"w_24h"`
	W7d   ModelWindow `json:"w_7d"`
	W30d  ModelWindow `json:"w_30d"`
}

// ModelRecent 模型近窗使用量（1h/24h/当天零点起），一次只扫近 24h 明细；
// Today 窗用于给日 × 模型聚合表的完整日数据补上当天的实时部分。
type ModelRecent struct {
	Model string      `json:"model"`
	W1h   ModelWindow `json:"w_1h"`
	W24h  ModelWindow `json:"w_24h"`
	Today ModelWindow `json:"today"`
}

// DayUsage 单日聚合使用量（含成功请求最大耗时）。
type DayUsage struct {
	Date          string `json:"date"` // YYYY-MM-DD
	Requests      int64  `json:"requests"`
	TotalTokens   int64  `json:"total_tokens"`
	MaxDurationMS int64  `json:"max_duration_ms"` // 当日成功请求最大耗时
}

// ModelDayPoint 单日单模型的 token 用量。
type ModelDayPoint struct {
	Date        string `json:"date"` // YYYY-MM-DD
	Model       string `json:"model"`
	TotalTokens int64  `json:"total_tokens"`
}

// SessionHeaderConfigRepository 会话标识配置数据访问接口。
type SessionHeaderConfigRepository interface {
	List(ctx context.Context) ([]model.SessionHeaderConfig, error)
	Create(ctx context.Context, config *model.SessionHeaderConfig) error
	Update(ctx context.Context, config *model.SessionHeaderConfig) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*model.SessionHeaderConfig, error)
	GetByKey(ctx context.Context, key string) (*model.SessionHeaderConfig, error)
	ListEnabled(ctx context.Context) ([]model.SessionHeaderConfig, error)
	Count(ctx context.Context, count *int64) error
}

// MemoryFilter 记忆检索/列表过滤条件（零值 = 不限命名空间与类型）。
type MemoryFilter struct {
	// Namespace 精确匹配命名空间名（空 = 不限）。
	Namespace string
	// Uncategorized 只看未分类（namespace=''）。
	Uncategorized bool
	// Kind 记忆类型（空 = 不限）。
	Kind string
	// Tags 标签过滤（全部命中，AND 语义）。
	Tags []string
	// Status 状态精确匹配（空 = 不限；服务层按场景补 active）。
	Status string
	// PinnedOnly 只看置顶。
	PinnedOnly bool
	// ExcludeExpired 排除已过期记忆（需 Now 非零）。
	ExcludeExpired bool
	// Now 过期判断基准时间（零值表示不判断过期）。
	Now time.Time
}

// MemoryHit 全文检索候选行（含 FTS5 bm25 得分，负值越小越相关）；
// LikeSearch 路径 Score 恒为 0，相关性由服务层按固定值处理。
type MemoryHit struct {
	model.Memory
	Score float64 `gorm:"column:score" json:"score"`
}

// MemoryStats 记忆库总览（控制台统计与命名空间图例用）。
type MemoryStats struct {
	Total         int64            `json:"total"`
	Active        int64            `json:"active"`
	Uncategorized int64            `json:"uncategorized"`
	Superseded    int64            `json:"superseded"`
	Archived      int64            `json:"archived"`
	Deleted       int64            `json:"deleted"`
	Links         int64            `json:"links"`
	ByKind        map[string]int64 `json:"by_kind"`
	ByNamespace   map[string]int64 `json:"by_namespace"`
}

// MemoryJobStatus 记忆分类任务状态（写回 memory_settings 的任务列）。
type MemoryJobStatus struct {
	Status        string     `json:"status"`         // 空 | running | done | failed
	Total         int        `json:"total"`          // 本轮待分类总数
	Done          int        `json:"done"`           // 已处理数
	Assigned      int        `json:"assigned"`       // 已归类数
	NewNamespaces int        `json:"new_namespaces"` // 新建命名空间数
	Message       string     `json:"message"`        // 结果摘要或错误信息
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

// MemoryRepository 记忆数据访问接口（memories 表 + memories_fts 索引）。
type MemoryRepository interface {
	// Create 写入记忆并同步 FTS 索引（同一事务）。
	Create(ctx context.Context, m *model.Memory) error
	// Update 全字段更新并同步 FTS 索引。
	Update(ctx context.Context, m *model.Memory) error
	// Get 按 id 读取；不存在返回 errs.CodeNotFound。
	Get(ctx context.Context, id int64) (*model.Memory, error)
	// GetByHash 同命名空间内按内容哈希查活跃记忆（写入去重）；不存在返回 gorm.ErrRecordNotFound。
	GetByHash(ctx context.Context, hash, namespace string) (*model.Memory, error)
	// ClearNamespace 把某命名空间的记忆移回未分类并清空归属来源，返回影响行数。
	ClearNamespace(ctx context.Context, namespace string) (int64, error)
	// Search FTS5 全文检索（match 为 textseg 构造的表达式），按相关性升序返回候选。
	Search(ctx context.Context, match string, f MemoryFilter, limit int) ([]MemoryHit, error)
	// LikeSearch 单字中文等无法进 FTS 的兜底检索（标题/正文子串匹配，按更新时间降序）。
	LikeSearch(ctx context.Context, needle string, f MemoryFilter, limit int) ([]MemoryHit, error)
	// List 分页列表；sort: updated | created | importance | access，空为 updated。
	List(ctx context.Context, f MemoryFilter, sort string, offset, limit int) ([]model.Memory, int64, error)
	// GraphNodes 关系图节点：置顶/重要度/访问热度优先，取前 limit 条（超限裁剪）。
	GraphNodes(ctx context.Context, f MemoryFilter, limit int) ([]model.Memory, error)
	// TouchAccess 批量刷新访问统计（检索即强化）。
	TouchAccess(ctx context.Context, ids []int64, at time.Time) error
	// NamespaceCounts 各命名空间的 active 记忆数（键 "" 为未分类）。
	NamespaceCounts(ctx context.Context) (map[string]int64, error)
	// Stats 记忆库总览。
	Stats(ctx context.Context) (*MemoryStats, error)
	// RebuildFTS 重建全文索引（修复用），返回重建行数。
	RebuildFTS(ctx context.Context) (int, error)
}

// MemoryNamespaceRepository 命名空间注册表数据访问接口。
type MemoryNamespaceRepository interface {
	// List 全部命名空间，按名称升序。
	List(ctx context.Context) ([]model.MemoryNamespace, error)
	// GetByName 按名称读取；不存在返回 errs.CodeNotFound。
	GetByName(ctx context.Context, name string) (*model.MemoryNamespace, error)
	// GetByID 按 id 读取；不存在返回 errs.CodeNotFound。
	GetByID(ctx context.Context, id int64) (*model.MemoryNamespace, error)
	// Ensure 不存在则创建（并发冲突吞掉），返回是否新建。
	Ensure(ctx context.Context, name, description, createdBy string) (bool, error)
	// Update 更新描述（名称变更走 Rename）。
	Update(ctx context.Context, ns *model.MemoryNamespace) error
	// Rename 事务内改名并同步记忆行；目标名已存在返回 errs.CodeInvalidParams。
	Rename(ctx context.Context, from, to string) (int64, error)
	// Merge 事务内把 from 的记忆并入 to 并删除 from 注册行。
	Merge(ctx context.Context, from, to string) (int64, error)
	// Delete 删除注册行并把其记忆移回未分类，返回迁移的记忆数。
	Delete(ctx context.Context, id int64) (int64, error)
}

// MemoryLinkRepository 记忆关系数据访问接口。
type MemoryLinkRepository interface {
	// Add 建边；已存在（from,to,relation）时幂等返回既有行。
	Add(ctx context.Context, l *model.MemoryLink) (*model.MemoryLink, error)
	// Delete 按 id 删边；不存在返回 errs.CodeNotFound。
	Delete(ctx context.Context, id int64) error
	// DeleteAutoByMemory 回收某条记忆发出的自动边（按建边者过滤，供自动关联重算用）。
	DeleteAutoByMemory(ctx context.Context, memoryID int64, createdBy string) (int64, error)
	// ListByMemory 与某条记忆相关的全部边。
	ListByMemory(ctx context.Context, id int64) ([]model.MemoryLink, error)
	// ListForNodes 两端都在 ids 内的边（关系图用）；ids 为空返回空表。
	ListForNodes(ctx context.Context, ids []int64) ([]model.MemoryLink, error)
	// Count 边总数。
	Count(ctx context.Context) (int64, error)
}

// MemorySettingsRepository 记忆库设置数据访问接口（单行表，ID 固定为 1）。
type MemorySettingsRepository interface {
	// Get 读取单行设置，不存在返回 gorm.ErrRecordNotFound。
	Get(ctx context.Context) (*model.MemorySettings, error)
	// Save 全量保存（首次播种用）。
	Save(ctx context.Context, s *model.MemorySettings) error
	// UpdateConfig 只更新分类配置列，避免覆盖任务写回的任务状态。
	UpdateConfig(ctx context.Context, s *model.MemorySettings) error
	// UpdateJobStatus 只更新任务状态列。
	UpdateJobStatus(ctx context.Context, st MemoryJobStatus) error
}
