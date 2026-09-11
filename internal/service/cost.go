package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
)

// CostOptions 计价服务运行参数。
type CostOptions struct {
	// Enabled 是否启用计价（关闭时价格表不出网、费用恒为 0）。
	Enabled bool
	// AutoSync 是否按 SyncInterval 定时同步公开价格表。
	AutoSync bool
	// SyncInterval 同步周期。
	SyncInterval time.Duration
	// SourceURL 价格表地址，空则用 provider.LiteLLMPricingURL。
	SourceURL string
	// CacheTTL 价格索引内存缓存时长（写路径会主动失效，此处只兜底）。
	CacheTTL time.Duration
	// InitialBilling 计费设置首次初始化入库的默认值（之后以设置页的值为准）。
	InitialBilling model.BillingSettings
}

const (
	defaultPriceCacheTTL = 5 * time.Minute
	// priceIndexFailureTTL 价格表读取失败时的短缓存，避免数据库故障时每个请求都重试。
	priceIndexFailureTTL = 10 * time.Second
	// recomputeBatch 费用重算单批行数。
	recomputeBatch = 500
	// defaultRecomputeDays 重算未指定起始时间时回看的默认天数（与默认保留期一致）。
	defaultRecomputeDays = 90
	// pricingLogModule 日志模块名。
	pricingLogModule = "pricing"
)

// PricingSource 价格表拉取来源，由 provider.PricingClient 实现；测试可注入假源。
type PricingSource interface {
	Fetch(ctx context.Context, url string) ([]provider.ModelPriceEntry, error)
}

// UsageInput 计价输入：已归一化的 token 计数 + 上游计算口径。
type UsageInput struct {
	PromptTokens     int64
	CompletionTokens int64
	CachedTokens     int64
	CacheWriteTokens int64
	Style            model.UsageStyle
}

// SplitInputTokens 按上游口径拆分输入侧 token，避免重复计费：
//   - openai    : prompt_tokens 已含缓存命中，未命中部分 = prompt - cached；OpenAI 不单独计缓存写
//   - anthropic : input_tokens 不含缓存读写，cached 与 cache_write 各自单独计费
//
// 口径判错会直接算错钱，因此新日志由网关在落库前写入真实口径（model.RequestLog.UsageStyle）。
func SplitInputTokens(u UsageInput) (uncachedIn, cached, cacheWrite int64) {
	if u.Style == model.UsageStyleAnthropic {
		return u.PromptTokens, u.CachedTokens, u.CacheWriteTokens
	}
	uncachedIn = u.PromptTokens - u.CachedTokens
	if uncachedIn < 0 {
		uncachedIn = 0
	}
	return uncachedIn, u.CachedTokens, 0
}

// Calculate 单次请求费用（USD）。
//
// at 用于判定时段价（命中空闲时段则用空闲费率）；p.Currency 为 CNY 时按 usdRate
// 折算成 USD，使 cost_usd 始终是单一币种，聚合层无需感知币种。
//
// 传零值 ModelPrice（未定价模型）返回 0；CNY 行但 usdRate <= 0（汇率未配置）同样返回 0，
// 宁可记 0 也不产出量纲错误的数字。
func Calculate(u UsageInput, p model.ModelPrice, at time.Time, usdRate float64) float64 {
	uncached, cached, cacheWrite := SplitInputTokens(u)
	in, out, readRate, writeRate := p.Rates(at, u.PromptTokens)
	sum := float64(uncached)*in +
		float64(cached)*readRate +
		float64(cacheWrite)*writeRate +
		float64(u.CompletionTokens)*out
	if p.IsCNY() {
		if usdRate <= 0 {
			return 0
		}
		return sum / usdRate
	}
	return sum
}

// ChannelProviderSource 渠道来源：存量日志重算费用时用于推断上游口径。
type ChannelProviderSource interface {
	ListAll(ctx context.Context) ([]model.Channel, error)
}

// CostService 计价业务：模型价格表 + 请求级费用计算 + 花费统计与预测。
//
// 网关热路径通过 Cost 计算费用，只读内存索引，不打数据库。
type CostService struct {
	prices  repository.ModelPriceRepository
	logs    repository.RequestLogRepository
	billing repository.BillingSettingsRepository
	source  PricingSource
	opts    CostOptions

	channels ChannelProviderSource

	mu       sync.RWMutex
	index    map[string]provider.ModelPriceEntry
	loadedAt time.Time
	curTTL   time.Duration
	stale    bool

	// 汇率缓存：热路径计费用，避免每请求查库。与价格索引同源失效。
	rateVal   float64
	rateAt    time.Time
	rateTTL   time.Duration
	rateStale bool

	// syncMu 保证同一时刻只有一次同步在跑（TryLock，不阻塞接口）。
	syncMu sync.Mutex
	// statusMu 保护同步状态字段，与 syncMu 分开以免状态查询被同步过程阻塞。
	statusMu    sync.RWMutex
	lastSyncAt  time.Time
	lastSyncErr string
	lastSyncNum int
}

// NewCostService 构造计价服务。
func NewCostService(
	prices repository.ModelPriceRepository,
	logs repository.RequestLogRepository,
	billing repository.BillingSettingsRepository,
	source PricingSource,
	opts CostOptions,
) *CostService {
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = defaultPriceCacheTTL
	}
	return &CostService{
		prices:  prices,
		logs:    logs,
		billing: billing,
		source:  source,
		opts:    opts,
		curTTL:  opts.CacheTTL,
	}
}

// SetChannels 注入渠道来源（nil = 重算时仅按入站协议推断口径）。
func (s *CostService) SetChannels(src ChannelProviderSource) { s.channels = src }

// EnsureDefaults 初始化计价基础设施：种子化计费设置、预热价格索引，
// 价格表为空时（首次启用）触发一次同步。同步失败只告警，不阻塞启动。
func (s *CostService) EnsureDefaults(ctx context.Context) error {
	if err := s.ensureBilling(ctx); err != nil {
		return err
	}
	s.invalidate()
	s.priceIndex(ctx)
	s.usdRate(ctx) // 预热汇率缓存，避免首批请求打库

	if !s.opts.Enabled {
		return nil
	}
	n, err := s.prices.Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := s.Sync(ctx); err != nil {
		logger.L(pricingLogModule).Warn("首次同步价格表失败，费用暂按 0 计", logger.Err(err))
	}
	return nil
}

func (s *CostService) ensureBilling(ctx context.Context) error {
	if _, err := s.billing.Get(ctx); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	seed := s.opts.InitialBilling
	switch strings.ToUpper(strings.TrimSpace(seed.DisplayCurrency)) {
	case model.CurrencyCNY:
		seed.DisplayCurrency = model.CurrencyCNY
	default:
		// CNY 但没有汇率时也退回 USD，避免展示出 ¥0
		seed.DisplayCurrency = model.CurrencyUSD
	}
	if seed.USDRate < 0 {
		seed.USDRate = 0
	}
	if seed.MonthlyBudgetUSD < 0 {
		seed.MonthlyBudgetUSD = 0
	}
	return s.billing.Save(ctx, &seed)
}

// Cost 计算单条请求费用（USD），并把命中的计价时段回写到 log.PricePeriod 供审计。
// 未定价模型、未启用计价均返回 0，不影响网关主流程。
func (s *CostService) Cost(l *model.RequestLog, style model.UsageStyle) float64 {
	if l == nil || !s.opts.Enabled {
		return 0
	}
	price, ok := s.lookup(l.Model)
	if !ok {
		return 0
	}
	// CreatedAt 在请求开始时就已写入；存量重算因此能按原始时刻正确判定时段。
	at := l.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	l.PricePeriod = price.PeakWindow.Period(at)
	return Calculate(UsageInput{
		PromptTokens:     l.PromptTokens,
		CompletionTokens: l.CompletionTokens,
		CachedTokens:     l.CachedTokens,
		CacheWriteTokens: l.CacheWriteTokens,
		Style:            style,
	}, price, at, s.usdRate(context.Background()))
}

// StyleOfLog 存量日志的上游口径推断（新日志由网关写入真实口径，不走这里）：
//  1. cache_write_tokens > 0 只可能来自 Anthropic（OpenAI 不上报缓存写），是最强信号
//  2. 原生透传时上游协议等于入站协议
//  3. 转换模式下按渠道 Provider 判定（openai_compatible 渠道即 OpenAI 口径）
//  4. 渠道已删除或未知时保守按入站协议推断
func StyleOfLog(l *model.RequestLog, providerByChannel map[int64]model.ProviderType) model.UsageStyle {
	if l.CacheWriteTokens > 0 {
		return model.UsageStyleAnthropic
	}
	if l.ForwardMode == model.ForwardNativePassthrough {
		return model.UsageStyleOf(l.Protocol)
	}
	if p, ok := providerByChannel[l.ChannelID]; ok {
		if p == model.ProviderAnthropic {
			return model.UsageStyleAnthropic
		}
		return model.UsageStyleOpenAI
	}
	return model.UsageStyleOf(l.Protocol)
}

// lookup 查模型价格（网关热路径，只读内存索引）。
func (s *CostService) lookup(name string) (model.ModelPrice, bool) {
	entry, ok := provider.MatchPrice(s.priceIndex(context.Background()), name)
	if !ok {
		return model.ModelPrice{}, false
	}
	return priceFromEntry(entry), true
}

// priceIndex 返回价格索引（内存缓存 + TTL + 显式失效，双检加锁）。
// 读取失败时短缓存已有内容，保证网关不受数据库故障拖垮。
func (s *CostService) priceIndex(ctx context.Context) map[string]provider.ModelPriceEntry {
	s.mu.RLock()
	if !s.stale && s.index != nil && time.Since(s.loadedAt) < s.curTTL {
		idx := s.index
		s.mu.RUnlock()
		return idx
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stale && s.index != nil && time.Since(s.loadedAt) < s.curTTL {
		return s.index
	}

	rows, err := s.prices.List(ctx)
	if err != nil {
		logger.L(pricingLogModule).Warn("读取价格表失败，本次按未定价处理", logger.Err(err))
		if s.index == nil {
			s.index = map[string]provider.ModelPriceEntry{}
		}
		s.loadedAt = time.Now()
		s.curTTL = priceIndexFailureTTL
		s.stale = false
		return s.index
	}
	entries := make([]provider.ModelPriceEntry, 0, len(rows))
	for _, p := range rows {
		entries = append(entries, entryFromPrice(p))
	}
	s.index = provider.IndexPrices(entries)
	s.loadedAt = time.Now()
	s.curTTL = s.opts.CacheTTL
	s.stale = false
	return s.index
}

// invalidate 价格表 / 计费设置变更后使缓存失效（价格索引与汇率）。
func (s *CostService) invalidate() {
	s.mu.Lock()
	s.stale = true
	s.rateStale = true
	s.mu.Unlock()
}

// usdRate 返回 USD→CNY 汇率（热路径，内存缓存 + TTL）。
// 返回 0 表示未配置：CNY 计价行在此时按 0 计，不产出量纲错误的费用。
func (s *CostService) usdRate(ctx context.Context) float64 {
	s.mu.RLock()
	if !s.rateStale && time.Since(s.rateAt) < s.rateTTL {
		v := s.rateVal
		s.mu.RUnlock()
		return v
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.rateStale && time.Since(s.rateAt) < s.rateTTL {
		return s.rateVal
	}

	b, err := s.billing.Get(ctx)
	if err != nil {
		// 记录不存在（尚未种子化）属正常，不告警。
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L(pricingLogModule).Warn("读取计费设置失败，汇率暂按 0 处理", logger.Err(err))
		}
		s.rateVal = 0
		s.rateAt = time.Now()
		s.rateTTL = priceIndexFailureTTL
		s.rateStale = false
		return 0
	}
	rate := b.USDRate
	if rate < 0 {
		rate = 0
	}
	s.rateVal = rate
	s.rateAt = time.Now()
	s.rateTTL = s.opts.CacheTTL
	s.rateStale = false
	return rate
}

// ---- 价格表同步 ----

// SyncResult 同步结果。
type SyncResult struct {
	Fetched       int       `json:"fetched"`        // 价格表中解析出的条目数
	Written       int       `json:"written"`        // 写入本地的行数
	SkippedManual int       `json:"skipped_manual"` // 因存在手工覆盖而跳过的行数
	SyncedAt      time.Time `json:"synced_at"`
	Source        string    `json:"source"`
}

// Sync 从公开价格表拉取并整体替换本地同步价格；手工配置行保留。
// 已有同步在跑时直接返回错误，避免并发拉取。
func (s *CostService) Sync(ctx context.Context) (*SyncResult, error) {
	if !s.opts.Enabled {
		return nil, errs.New(errs.CodeInvalidParams, "计价功能未启用（configs/config.yaml 的 pricing.enabled）")
	}
	if !s.syncMu.TryLock() {
		return nil, errs.New(errs.CodeInvalidParams, "价格表同步正在进行中，请稍候")
	}
	defer s.syncMu.Unlock()

	source := s.opts.SourceURL
	if strings.TrimSpace(source) == "" {
		source = provider.LiteLLMPricingURL
	}
	entries, err := s.source.Fetch(ctx, source)
	if err != nil {
		s.recordSyncError(err)
		return nil, errs.Wrap(errs.CodeInternal, "同步价格表失败", err)
	}

	now := time.Now()
	prices := make([]model.ModelPrice, 0, len(entries))
	for _, e := range entries {
		p := priceFromEntry(e)
		p.Source = model.PriceFromSynced
		// 公开价格表只有美元价、无时段价：同步行固定 USD 且不带空闲/时段配置。
		p.Currency = model.CurrencyUSD
		p.PeakWindow = model.PeakWindow{}
		p.OffPeakInputCostPerToken = 0
		p.OffPeakOutputCostPerToken = 0
		p.OffPeakCacheReadCostPerToken = 0
		p.OffPeakCacheWriteCostPerToken = 0
		syncedAt := now
		p.SyncedAt = &syncedAt
		prices = append(prices, p)
	}

	written, skipped, err := s.prices.ReplaceSynced(ctx, prices)
	if err != nil {
		s.recordSyncError(err)
		return nil, errs.Wrap(errs.CodeInternal, "写入价格表失败", err)
	}
	s.invalidate()
	s.recordSyncOK(written, now)

	logger.L(pricingLogModule).Info("价格表同步完成",
		"fetched", len(entries), "written", written, "skipped_manual", skipped, "source", source)
	return &SyncResult{
		Fetched: len(entries), Written: written, SkippedManual: skipped,
		SyncedAt: now, Source: source,
	}, nil
}

func (s *CostService) recordSyncError(err error) {
	s.statusMu.Lock()
	s.lastSyncErr = err.Error()
	s.statusMu.Unlock()
	logger.L(pricingLogModule).Warn("价格表同步失败", logger.Err(err))
}

func (s *CostService) recordSyncOK(written int, at time.Time) {
	s.statusMu.Lock()
	s.lastSyncAt = at
	s.lastSyncErr = ""
	s.lastSyncNum = written
	s.statusMu.Unlock()
}

// RunAutoSync 后台定时同步，直到 ctx 取消。未启用或间隔非法时立即返回。
func (s *CostService) RunAutoSync(ctx context.Context) {
	if !s.opts.Enabled || !s.opts.AutoSync || s.opts.SyncInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.opts.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 失败与「已在同步中」都在 Sync 内部记过日志，这里无需重复处理
			_, _ = s.Sync(ctx)
		}
	}
}

// SyncStatus 同步状态与价格表规模，供设置页展示。
type SyncStatus struct {
	Enabled         bool       `json:"enabled"`
	AutoSync        bool       `json:"auto_sync"`
	SourceURL       string     `json:"source_url"`
	IntervalHours   float64    `json:"interval_hours"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	LastSyncedCount int        `json:"last_synced_count"`
	PriceCount      int64      `json:"price_count"`
}

// Status 返回同步状态。
func (s *CostService) Status(ctx context.Context) *SyncStatus {
	source := s.opts.SourceURL
	if strings.TrimSpace(source) == "" {
		source = provider.LiteLLMPricingURL
	}
	st := &SyncStatus{
		Enabled:       s.opts.Enabled,
		AutoSync:      s.opts.AutoSync,
		SourceURL:     source,
		IntervalHours: s.opts.SyncInterval.Hours(),
	}
	s.statusMu.RLock()
	if !s.lastSyncAt.IsZero() {
		at := s.lastSyncAt
		st.LastSyncAt = &at
	}
	st.LastError = s.lastSyncErr
	st.LastSyncedCount = s.lastSyncNum
	s.statusMu.RUnlock()

	if n, err := s.prices.Count(ctx); err == nil {
		st.PriceCount = n
	}
	return st
}

// ---- 费用重算 ----

// RecomputeResult 重算结果。
type RecomputeResult struct {
	Scanned int       `json:"scanned"`
	Updated int       `json:"updated"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
}

// Recompute 重算时间窗内的请求费用。onlyMissing 为 true 时只补算 cost_usd <= 0 的历史行，
// 为 false 时全量重算（价格调整后使用）。from 为零值时回看默认保留期。
func (s *CostService) Recompute(ctx context.Context, from, to time.Time, onlyMissing bool) (*RecomputeResult, error) {
	if !s.opts.Enabled {
		return nil, errs.New(errs.CodeInvalidParams, "计价功能未启用（configs/config.yaml 的 pricing.enabled）")
	}
	if from.IsZero() {
		from = time.Now().AddDate(0, 0, -defaultRecomputeDays)
	}
	if !to.IsZero() && to.Before(from) {
		return nil, errs.New(errs.CodeInvalidParams, "结束时间早于起始时间")
	}

	providers := s.channelProviders(ctx)
	// 响应里的 to 用实际生效的上界，避免前端拿到零值时间
	effectiveTo := to
	if effectiveTo.IsZero() {
		effectiveTo = time.Now()
	}
	res := &RecomputeResult{From: from, To: effectiveTo}
	var afterID int64
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		batch, err := s.logs.RecomputeBatch(ctx, from, to, afterID, recomputeBatch, onlyMissing)
		if err != nil {
			return res, errs.Wrap(errs.CodeInternal, "读取待重算日志失败", err)
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			l := &batch[i]
			afterID = l.ID
			res.Scanned++

			style := StyleOfLog(l, providers)
			prevPeriod := l.PricePeriod
			// Cost 会把命中的计价时段写入 l.PricePeriod（按 CreatedAt 判定原始时刻），
			// 因此原值需先留存，供幂等比较。
			cost := s.Cost(l, style)
			if cost == l.CostUSD && style == l.UsageStyle && l.PricePeriod == prevPeriod {
				continue // 幂等：无变化不写库
			}
			if err := s.logs.UpdateCost(ctx, l.ID, cost, style, l.PricePeriod); err != nil {
				return res, errs.Wrap(errs.CodeInternal, "回写费用失败", err)
			}
			res.Updated++
		}
		if len(batch) < recomputeBatch {
			break
		}
	}
	logger.L(pricingLogModule).Info("费用重算完成",
		"scanned", res.Scanned, "updated", res.Updated, "only_missing", onlyMissing)
	return res, nil
}

func (s *CostService) channelProviders(ctx context.Context) map[int64]model.ProviderType {
	if s.channels == nil {
		return nil
	}
	chans, err := s.channels.ListAll(ctx)
	if err != nil {
		logger.L(pricingLogModule).Warn("读取渠道列表失败，口径按入站协议推断", logger.Err(err))
		return nil
	}
	out := make(map[int64]model.ProviderType, len(chans))
	for _, ch := range chans {
		out[ch.ID] = ch.Provider
	}
	return out
}

// ---- 价格表 CRUD ----

// PriceInput 价格录入/编辑入参（费率单位：Currency 对应的单 token 金额）。
type PriceInput struct {
	Model string `json:"model"`
	// Currency 费率币种：USD（默认）| CNY。
	Currency string `json:"currency"`

	InputCostPerToken      float64 `json:"input_cost_per_token"`
	OutputCostPerToken     float64 `json:"output_cost_per_token"`
	CacheReadCostPerToken  float64 `json:"cache_read_cost_per_token"`
	CacheWriteCostPerToken float64 `json:"cache_write_cost_per_token"`

	// PeakWindow 高峰时段规则，如 `1-5;09:00-12:00,14:00-18:00`（北京时间）；
	// 空 = 不启用时段价。OffPeak* 为对应空闲时段费率（同 Currency）。
	PeakWindow                    string  `json:"peak_window"`
	OffPeakInputCostPerToken      float64 `json:"off_peak_input_cost_per_token"`
	OffPeakOutputCostPerToken     float64 `json:"off_peak_output_cost_per_token"`
	OffPeakCacheReadCostPerToken  float64 `json:"off_peak_cache_read_cost_per_token"`
	OffPeakCacheWriteCostPerToken float64 `json:"off_peak_cache_write_cost_per_token"`

	ThresholdTokens         int64   `json:"threshold_tokens"`
	InputCostAbovePerToken  float64 `json:"input_cost_above_per_token"`
	OutputCostAbovePerToken float64 `json:"output_cost_above_per_token"`
	CacheReadAbovePerToken  float64 `json:"cache_read_cost_above_per_token"`
	CacheWriteAbovePerToken float64 `json:"cache_write_cost_above_per_token"`
}

// ListPrices 分页查询价格表（q 模糊匹配模型名，usedOnly 只返回有用量的模型）。
func (s *CostService) ListPrices(ctx context.Context, q string, usedOnly bool, offset, limit int) ([]model.ModelPrice, int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.prices.ListPaged(ctx, q, usedOnly, offset, limit)
}

// CreatePrice 手工新增价格（source=manual，同步不覆盖）。
func (s *CostService) CreatePrice(ctx context.Context, in *PriceInput) (*model.ModelPrice, error) {
	name := strings.TrimSpace(in.Model)
	if name == "" {
		return nil, errs.New(errs.CodeInvalidParams, "模型名不能为空")
	}
	if err := validateRates(in); err != nil {
		return nil, err
	}
	if _, err := s.prices.GetByModel(ctx, name); err == nil {
		return nil, errs.New(errs.CodeInvalidParams, "该模型已有价格，请直接编辑")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	p := &model.ModelPrice{Model: name, Source: model.PriceFromManual}
	if err := applyPriceInput(p, in); err != nil {
		return nil, err
	}
	if err := s.prices.Create(ctx, p); err != nil {
		return nil, err
	}
	s.invalidate()
	return p, nil
}

// UpdatePrice 更新价格；更新后该行标记为手工配置，后续同步不会覆盖。
func (s *CostService) UpdatePrice(ctx context.Context, id int64, in *PriceInput) (*model.ModelPrice, error) {
	if err := validateRates(in); err != nil {
		return nil, err
	}
	p, err := s.prices.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "价格不存在")
		}
		return nil, err
	}
	if err := applyPriceInput(p, in); err != nil {
		return nil, err
	}
	p.Source = model.PriceFromManual
	if err := s.prices.Update(ctx, p); err != nil {
		return nil, err
	}
	s.invalidate()
	return p, nil
}

// DeletePrice 删除价格行。同步来源的行会在下次同步时重新写回。
func (s *CostService) DeletePrice(ctx context.Context, id int64) error {
	if err := s.prices.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// unpricedScanLimit 取用量模型时的扫描上限：别名命中的模型会被过滤掉，
// 需要比返回条数更多的样本才能填满 limit。
const unpricedScanLimit = 200

// UnpricedModels 有用量但价格表未覆盖的模型。
//
// 判定复用计价热路径的 MatchPrice（含去厂商前缀等别名规则），而不是价格表的
// 精确同名匹配：LiteLLM 的价格键常带厂商前缀（如 zai/glm-5.3-flash），
// 精确匹配会把实际已正确计费的模型误报为未定价。
func (s *CostService) UnpricedModels(ctx context.Context, since time.Time, limit int) ([]repository.ModelUsage, error) {
	if !s.opts.Enabled {
		return []repository.ModelUsage{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	items, err := s.logs.ModelsWithUsage(ctx, since, unpricedScanLimit)
	if err != nil {
		return nil, err
	}
	index := s.priceIndex(ctx)
	out := make([]repository.ModelUsage, 0, len(items))
	for _, it := range items {
		if _, ok := provider.MatchPrice(index, it.Model); ok {
			continue
		}
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Billing 读取计费展示设置（不存在时返回默认值）。
func (s *CostService) Billing(ctx context.Context) (*model.BillingSettings, error) {
	cur, err := s.billing.Get(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &model.BillingSettings{ID: 1, DisplayCurrency: model.CurrencyUSD}, nil
		}
		return nil, err
	}
	return cur, nil
}

// UpdateBillingInput 计费设置更新入参。
type UpdateBillingInput struct {
	DisplayCurrency  string  `json:"display_currency"`
	USDRate          float64 `json:"usd_cny_rate"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
}

// UpdateBilling 更新计费展示设置。
func (s *CostService) UpdateBilling(ctx context.Context, in *UpdateBillingInput) (*model.BillingSettings, error) {
	cur, err := s.Billing(ctx)
	if err != nil {
		return nil, err
	}
	cur.ID = 1
	switch strings.ToUpper(strings.TrimSpace(in.DisplayCurrency)) {
	case model.CurrencyCNY:
		cur.DisplayCurrency = model.CurrencyCNY
	case model.CurrencyUSD, "":
		cur.DisplayCurrency = model.CurrencyUSD
	default:
		return nil, errs.New(errs.CodeInvalidParams, "展示币种只支持 USD 或 CNY")
	}
	if in.USDRate < 0 {
		return nil, errs.New(errs.CodeInvalidParams, "汇率不能为负数")
	}
	if cur.DisplayCurrency == model.CurrencyCNY && in.USDRate <= 0 {
		return nil, errs.New(errs.CodeInvalidParams, "展示币种为 CNY 时需填写大于 0 的汇率")
	}
	cur.USDRate = in.USDRate
	if in.MonthlyBudgetUSD < 0 {
		return nil, errs.New(errs.CodeInvalidParams, "月度预算不能为负数")
	}
	cur.MonthlyBudgetUSD = in.MonthlyBudgetUSD
	if err := s.billing.Save(ctx, cur); err != nil {
		return nil, err
	}
	// 汇率影响 CNY 计价行的折算，必须立即失效热路径缓存，否则改完最长一个 TTL 才生效。
	s.invalidate()
	return cur, nil
}

func applyPriceInput(p *model.ModelPrice, in *PriceInput) error {
	cur := strings.ToUpper(strings.TrimSpace(in.Currency))
	switch cur {
	case model.CurrencyCNY:
		p.Currency = model.CurrencyCNY
	default:
		p.Currency = model.CurrencyUSD
	}
	p.InputCostPerToken = in.InputCostPerToken
	p.OutputCostPerToken = in.OutputCostPerToken
	p.CacheReadCostPerToken = in.CacheReadCostPerToken
	p.CacheWriteCostPerToken = in.CacheWriteCostPerToken

	win, err := model.NewPeakWindow(in.PeakWindow)
	if err != nil {
		return errs.New(errs.CodeInvalidParams, err.Error())
	}
	p.PeakWindow = win
	p.OffPeakInputCostPerToken = in.OffPeakInputCostPerToken
	p.OffPeakOutputCostPerToken = in.OffPeakOutputCostPerToken
	p.OffPeakCacheReadCostPerToken = in.OffPeakCacheReadCostPerToken
	p.OffPeakCacheWriteCostPerToken = in.OffPeakCacheWriteCostPerToken

	p.ThresholdTokens = in.ThresholdTokens
	p.InputCostAbovePerToken = in.InputCostAbovePerToken
	p.OutputCostAbovePerToken = in.OutputCostAbovePerToken
	p.CacheReadAbovePerToken = in.CacheReadAbovePerToken
	p.CacheWriteAbovePerToken = in.CacheWriteAbovePerToken
	return nil
}

func validateRates(in *PriceInput) error {
	if cur := strings.ToUpper(strings.TrimSpace(in.Currency)); cur != "" &&
		cur != model.CurrencyUSD && cur != model.CurrencyCNY {
		return errs.New(errs.CodeInvalidParams, "价格币种只支持 USD 或 CNY")
	}
	checks := []struct {
		name  string
		value float64
	}{
		{"输入", in.InputCostPerToken},
		{"输出", in.OutputCostPerToken},
		{"缓存读", in.CacheReadCostPerToken},
		{"缓存写", in.CacheWriteCostPerToken},
		{"空闲输入", in.OffPeakInputCostPerToken},
		{"空闲输出", in.OffPeakOutputCostPerToken},
		{"空闲缓存读", in.OffPeakCacheReadCostPerToken},
		{"空闲缓存写", in.OffPeakCacheWriteCostPerToken},
		{"超阈值输入", in.InputCostAbovePerToken},
		{"超阈值输出", in.OutputCostAbovePerToken},
	}
	for _, c := range checks {
		if c.value < 0 {
			return errs.New(errs.CodeInvalidParams, c.name+"单价不能为负数")
		}
	}
	if in.ThresholdTokens < 0 {
		return errs.New(errs.CodeInvalidParams, "长上下文阈值不能为负数")
	}
	if in.InputCostPerToken <= 0 && in.OutputCostPerToken <= 0 {
		return errs.New(errs.CodeInvalidParams, "输入与输出单价至少填写一项")
	}
	// 时段规则先校验语法：非法规则若落库，会在每次计费时反复失败。
	if _, err := model.NewPeakWindow(in.PeakWindow); err != nil {
		return errs.New(errs.CodeInvalidParams, err.Error())
	}
	return nil
}

// entryFromPrice 价格表行 -> 内存索引条目。
func entryFromPrice(p model.ModelPrice) provider.ModelPriceEntry {
	return provider.ModelPriceEntry{
		Model:                   p.Model,
		InputCostPerToken:       p.InputCostPerToken,
		OutputCostPerToken:      p.OutputCostPerToken,
		CacheReadCostPerToken:   p.CacheReadCostPerToken,
		CacheWriteCostPerToken:  p.CacheWriteCostPerToken,
		ThresholdTokens:         p.ThresholdTokens,
		InputCostAbovePerToken:  p.InputCostAbovePerToken,
		OutputCostAbovePerToken: p.OutputCostAbovePerToken,
		CacheReadAbovePerToken:  p.CacheReadAbovePerToken,
		CacheWriteAbovePerToken: p.CacheWriteAbovePerToken,
		Provider:                p.Provider,

		Currency:                      p.CurrencyOrDefault(),
		PeakWindow:                    p.PeakWindow,
		OffPeakInputCostPerToken:      p.OffPeakInputCostPerToken,
		OffPeakOutputCostPerToken:     p.OffPeakOutputCostPerToken,
		OffPeakCacheReadCostPerToken:  p.OffPeakCacheReadCostPerToken,
		OffPeakCacheWriteCostPerToken: p.OffPeakCacheWriteCostPerToken,
	}
}

// priceFromEntry 内存索引条目 -> 价格模型（同步写入与热路径计价共用）。
func priceFromEntry(e provider.ModelPriceEntry) model.ModelPrice {
	return model.ModelPrice{
		Model:                   e.Model,
		InputCostPerToken:       e.InputCostPerToken,
		OutputCostPerToken:      e.OutputCostPerToken,
		CacheReadCostPerToken:   e.CacheReadCostPerToken,
		CacheWriteCostPerToken:  e.CacheWriteCostPerToken,
		ThresholdTokens:         e.ThresholdTokens,
		InputCostAbovePerToken:  e.InputCostAbovePerToken,
		OutputCostAbovePerToken: e.OutputCostAbovePerToken,
		CacheReadAbovePerToken:  e.CacheReadAbovePerToken,
		CacheWriteAbovePerToken: e.CacheWriteAbovePerToken,
		Provider:                e.Provider,

		Currency:                      e.Currency,
		PeakWindow:                    e.PeakWindow,
		OffPeakInputCostPerToken:      e.OffPeakInputCostPerToken,
		OffPeakOutputCostPerToken:     e.OffPeakOutputCostPerToken,
		OffPeakCacheReadCostPerToken:  e.OffPeakCacheReadCostPerToken,
		OffPeakCacheWriteCostPerToken: e.OffPeakCacheWriteCostPerToken,
	}
}
