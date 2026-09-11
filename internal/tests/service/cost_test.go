package service_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
	"xtokenhub/internal/repository"
	"xtokenhub/internal/service"
	"xtokenhub/internal/tests/testutil"
)

// 测试费率：输入 3 USD/M、输出 15 USD/M、缓存读 0.3 USD/M、缓存写 3.75 USD/M（USD/单 token）。
const (
	rateIn     = 0.000003
	rateOut    = 0.000015
	rateRead   = 0.0000003
	rateWrite  = 0.00000375
	epsilonNum = 1e-12
	// testUSDRate 测试用 USD→CNY 汇率。
	testUSDRate = 7.2
)

// 北京时间基准时刻：2026-01-05 是周一，01-03 是周六。
// 用固定日期而非 time.Now()，时段判定才是确定性的。
var (
	// atPeak 周一 10:00（工作日高峰）。
	atPeak = time.Date(2026, 1, 5, 10, 0, 0, 0, model.BeijingZone)
	// atLunchBreak 周一 13:00（午休，属空闲）。
	atLunchBreak = time.Date(2026, 1, 5, 13, 0, 0, 0, model.BeijingZone)
	// atWeekend 周六 10:00（周末，属空闲）。
	atWeekend = time.Date(2026, 1, 3, 10, 0, 0, 0, model.BeijingZone)
	// atEvening 周一 20:00（晚间，属空闲）。
	atEvening = time.Date(2026, 1, 5, 20, 0, 0, 0, model.BeijingZone)
)

// deepseekPeakWindow DeepSeek 官方错峰规则：工作日 9-12、14-18 为高峰。
const deepseekPeakWindow = "1-5;09:00-12:00,14:00-18:00"

func approx(t *testing.T, got, want float64, msg string) {
	t.Helper()
	approxTol(t, got, want, epsilonNum, msg)
}

// approxTol 带容差比较：涉及 time.Now() 参与运算的预测值需要放宽容差。
func approxTol(t *testing.T, got, want, tol float64, msg string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

func sonnetEntry() provider.ModelPriceEntry {
	return provider.ModelPriceEntry{
		Model: "claude-sonnet-4", Provider: "anthropic",
		InputCostPerToken: rateIn, OutputCostPerToken: rateOut,
		CacheReadCostPerToken: rateRead, CacheWriteCostPerToken: rateWrite,
		ThresholdTokens:        200000,
		InputCostAbovePerToken: 0.000006, OutputCostAbovePerToken: 0.0000225,
	}
}

// priceModel 把价格条目转成计价用的模型价格（与同步入表口径一致）。
func priceModel(e provider.ModelPriceEntry) model.ModelPrice {
	return model.ModelPrice{
		Model: e.Model, Provider: e.Provider,
		Currency:              e.Currency,
		InputCostPerToken:     e.InputCostPerToken,
		OutputCostPerToken:    e.OutputCostPerToken,
		CacheReadCostPerToken: e.CacheReadCostPerToken, CacheWriteCostPerToken: e.CacheWriteCostPerToken,
		PeakWindow:                    e.PeakWindow,
		OffPeakInputCostPerToken:      e.OffPeakInputCostPerToken,
		OffPeakOutputCostPerToken:     e.OffPeakOutputCostPerToken,
		OffPeakCacheReadCostPerToken:  e.OffPeakCacheReadCostPerToken,
		OffPeakCacheWriteCostPerToken: e.OffPeakCacheWriteCostPerToken,
		ThresholdTokens:               e.ThresholdTokens,
		InputCostAbovePerToken:        e.InputCostAbovePerToken,
		OutputCostAbovePerToken:       e.OutputCostAbovePerToken,
		CacheReadAbovePerToken:        e.CacheReadAbovePerToken,
		CacheWriteAbovePerToken:       e.CacheWriteAbovePerToken,
	}
}

func newCostService(t *testing.T, entries ...provider.ModelPriceEntry) (*service.CostService, repository.ModelPriceRepository, repository.RequestLogRepository) {
	t.Helper()
	db := testutil.NewMemoryDB(t)
	priceRepo := repository.NewModelPriceRepository(db)
	logRepo := repository.NewRequestLogRepository(db)
	billingRepo := repository.NewBillingSettingsRepository(db)
	// 把初始计费设置真正写入库：人民币计价行要读汇率才能折算，而热路径读的是库里的值。
	if err := billingRepo.Save(context.Background(), &model.BillingSettings{
		DisplayCurrency: model.CurrencyUSD, USDRate: testUSDRate,
	}); err != nil {
		t.Fatalf("种子化计费设置: %v", err)
	}
	svc := service.NewCostService(
		priceRepo, logRepo, billingRepo,
		testutil.NewStubPricingSource(entries...),
		service.CostOptions{Enabled: true, SyncInterval: time.Hour,
			InitialBilling: model.BillingSettings{DisplayCurrency: model.CurrencyUSD, USDRate: testUSDRate}},
	)
	return svc, priceRepo, logRepo
}

// ---- 计价公式 ----

func TestSplitInputTokens(t *testing.T) {
	cases := []struct {
		name                         string
		in                           service.UsageInput
		uncached, cached, cacheWrite int64
	}{
		{
			// OpenAI：prompt 已含 cached，未命中部分要减掉，且不单独计缓存写
			name:     "openai 口径扣减缓存命中",
			in:       service.UsageInput{PromptTokens: 100, CachedTokens: 40, CacheWriteTokens: 7, Style: model.UsageStyleOpenAI},
			uncached: 60, cached: 40, cacheWrite: 0,
		},
		{
			// 上游异常给出 cached > prompt 时不应算出负的未命中数
			name:     "openai 口径 cached 超过 prompt 时钳零",
			in:       service.UsageInput{PromptTokens: 10, CachedTokens: 30, Style: model.UsageStyleOpenAI},
			uncached: 0, cached: 30, cacheWrite: 0,
		},
		{
			// Anthropic：input_tokens 不含缓存读写，三者相加才是全部输入
			name:     "anthropic 口径三者独立",
			in:       service.UsageInput{PromptTokens: 100, CachedTokens: 40, CacheWriteTokens: 7, Style: model.UsageStyleAnthropic},
			uncached: 100, cached: 40, cacheWrite: 7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, ca, cw := service.SplitInputTokens(c.in)
			if u != c.uncached || ca != c.cached || cw != c.cacheWrite {
				t.Errorf("= (%d,%d,%d), want (%d,%d,%d)", u, ca, cw, c.uncached, c.cached, c.cacheWrite)
			}
		})
	}
}

func TestCalculate(t *testing.T) {
	cases := []struct {
		name string
		in   service.UsageInput
		want float64
		zero bool // 传零值价格（模拟未定价模型）
	}{
		{
			// anthropic：100 未命中*3e-6 + 40 缓存读*3e-7 + 7 缓存写*3.75e-6 + 20 输出*1.5e-5
			name: "anthropic 缓存读写独立计费",
			in:   service.UsageInput{PromptTokens: 100, CompletionTokens: 20, CachedTokens: 40, CacheWriteTokens: 7, Style: model.UsageStyleAnthropic},
			want: 100*rateIn + 40*rateRead + 7*rateWrite + 20*rateOut,
		},
		{
			// openai：(100-40)*3e-6 + 40*3e-7 + 20*1.5e-5，缓存写忽略
			name: "openai 缓存命中扣减且不计缓存写",
			in:   service.UsageInput{PromptTokens: 100, CompletionTokens: 20, CachedTokens: 40, CacheWriteTokens: 7, Style: model.UsageStyleOpenAI},
			want: 60*rateIn + 40*rateRead + 20*rateOut,
		},
		{
			// prompt 250k > 阈值 200k：整单改用超阈值费率
			name: "超过长上下文阈值改用分档费率",
			in:   service.UsageInput{PromptTokens: 250000, CompletionTokens: 10, Style: model.UsageStyleAnthropic},
			want: 250000*0.000006 + 10*0.0000225,
		},
		{
			name: "阈值边界上不启用分档",
			in:   service.UsageInput{PromptTokens: 200000, CompletionTokens: 0, Style: model.UsageStyleAnthropic},
			want: 200000 * rateIn,
		},
		{
			name: "未定价模型（零价格）费用为 0",
			in:   service.UsageInput{PromptTokens: 100, CompletionTokens: 100, Style: model.UsageStyleOpenAI},
			want: 0,
			zero: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := priceModel(sonnetEntry())
			if c.zero {
				p = model.ModelPrice{}
			}
			approx(t, service.Calculate(c.in, p, atPeak, testUSDRate), c.want, c.name)
		})
	}
}

// 缓存费率缺失时回退输入费率（与公开价格表口径一致）。
func TestCalculateCacheRateFallback(t *testing.T) {
	p := priceModel(provider.ModelPriceEntry{
		Model: "m", InputCostPerToken: rateIn, OutputCostPerToken: rateOut,
	})
	got := service.Calculate(service.UsageInput{
		PromptTokens: 10, CachedTokens: 4, CacheWriteTokens: 2, Style: model.UsageStyleAnthropic,
	}, p, atPeak, testUSDRate)
	// 10*3e-6 + 4*3e-6（回退输入费率）+ 2*3e-6（回退输入费率）
	approx(t, got, 16*rateIn, "缓存费率回退")
}

// ---- StyleOfLog 存量日志口径推断 ----

func TestStyleOfLog(t *testing.T) {
	providers := map[int64]model.ProviderType{
		1: model.ProviderAnthropic,
		2: model.ProviderOpenAICompatible,
	}
	cases := []struct {
		name string
		log  model.RequestLog
		want model.UsageStyle
	}{
		{
			name: "有缓存写一定是 anthropic（OpenAI 不上报）",
			log:  model.RequestLog{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardConverted, ChannelID: 2, CacheWriteTokens: 3},
			want: model.UsageStyleAnthropic,
		},
		{
			name: "原生透传按入站协议",
			log:  model.RequestLog{Protocol: model.ProtocolMessages, ForwardMode: model.ForwardNativePassthrough, ChannelID: 2},
			want: model.UsageStyleAnthropic,
		},
		{
			name: "原生透传 chat 为 openai",
			log:  model.RequestLog{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough, ChannelID: 1},
			want: model.UsageStyleOpenAI,
		},
		{
			name: "转换模式且渠道是 anthropic 上游",
			log:  model.RequestLog{Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardConverted, ChannelID: 1},
			want: model.UsageStyleAnthropic,
		},
		{
			name: "转换模式且渠道是 openai 上游",
			log:  model.RequestLog{Protocol: model.ProtocolMessages, ForwardMode: model.ForwardConverted, ChannelID: 2},
			want: model.UsageStyleOpenAI,
		},
		{
			name: "渠道已删除时保守按入站协议",
			log:  model.RequestLog{Protocol: model.ProtocolMessages, ForwardMode: model.ForwardConverted, ChannelID: 99},
			want: model.UsageStyleAnthropic,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := service.StyleOfLog(&c.log, providers); got != c.want {
				t.Errorf("= %q, want %q", got, c.want)
			}
		})
	}
}

// ---- CostService：热路径计价 ----

func TestCostServiceCost(t *testing.T) {
	svc, _, _ := newCostService(t, sonnetEntry())
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("同步价格表: %v", err)
	}

	log := &model.RequestLog{
		Model: "claude-sonnet-4", Protocol: model.ProtocolMessages,
		PromptTokens: 100, CompletionTokens: 20, CachedTokens: 40, CacheWriteTokens: 7,
	}
	approx(t, svc.Cost(log, model.UsageStyleAnthropic),
		100*rateIn+40*rateRead+7*rateWrite+20*rateOut, "anthropic 计价")

	// 未定价模型记 0，不应报错
	if got := svc.Cost(&model.RequestLog{Model: "unknown-model", PromptTokens: 100}, model.UsageStyleOpenAI); got != 0 {
		t.Errorf("未定价模型 = %v, want 0", got)
	}
	// nil 安全
	if got := svc.Cost(nil, model.UsageStyleOpenAI); got != 0 {
		t.Errorf("nil log = %v, want 0", got)
	}
}

// 计价关闭时费用恒为 0，且 Sync / Recompute 明确报错。
func TestCostServiceDisabled(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	svc := service.NewCostService(
		repository.NewModelPriceRepository(db),
		repository.NewRequestLogRepository(db),
		repository.NewBillingSettingsRepository(db),
		testutil.NewStubPricingSource(sonnetEntry()),
		service.CostOptions{Enabled: false},
	)
	if got := svc.Cost(&model.RequestLog{Model: "claude-sonnet-4", PromptTokens: 100}, model.UsageStyleAnthropic); got != 0 {
		t.Errorf("关闭计价时费用 = %v, want 0", got)
	}
	if _, err := svc.Sync(context.Background()); err == nil {
		t.Error("关闭计价时 Sync 应报错")
	}
	if _, err := svc.Recompute(context.Background(), time.Time{}, time.Time{}, true); err == nil {
		t.Error("关闭计价时 Recompute 应报错")
	}
}

// ---- 价格表 CRUD 与同步覆盖规则 ----

func TestPriceCRUDAndManualProtection(t *testing.T) {
	svc, priceRepo, _ := newCostService(t, sonnetEntry())
	ctx := context.Background()

	// 同步写入
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	n, _ := priceRepo.Count(ctx)
	if n != 1 {
		t.Fatalf("价格表行数 = %d, want 1", n)
	}

	// 手工新增：校验
	if _, err := svc.CreatePrice(ctx, &service.PriceInput{Model: "  ", InputCostPerToken: 1}); err == nil {
		t.Error("空模型名应报错")
	}
	if _, err := svc.CreatePrice(ctx, &service.PriceInput{Model: "x", InputCostPerToken: -1}); err == nil {
		t.Error("负单价应报错")
	}
	if _, err := svc.CreatePrice(ctx, &service.PriceInput{Model: "x"}); err == nil {
		t.Error("输入输出都为空应报错")
	}
	if _, err := svc.CreatePrice(ctx, &service.PriceInput{Model: "claude-sonnet-4", InputCostPerToken: 1}); err == nil {
		t.Error("重复模型应报错")
	}

	created, err := svc.CreatePrice(ctx, &service.PriceInput{
		Model: "my-model", InputCostPerToken: 0.000001, OutputCostPerToken: 0.000002,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Source != model.PriceFromManual {
		t.Errorf("手工新增 source = %q, want manual", created.Source)
	}

	// 把同步来的行改成手工配置
	synced, err := priceRepo.GetByModel(ctx, "claude-sonnet-4")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdatePrice(ctx, synced.ID, &service.PriceInput{
		InputCostPerToken: 0.000009, OutputCostPerToken: 0.00009,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Source != model.PriceFromManual {
		t.Errorf("编辑后 source = %q, want manual", updated.Source)
	}

	// 再次同步：手工行必须原样保留
	res, err := svc.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.SkippedManual != 1 {
		t.Errorf("跳过手工行 = %d, want 1", res.SkippedManual)
	}
	kept, err := priceRepo.GetByModel(ctx, "claude-sonnet-4")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Source != model.PriceFromManual || kept.InputCostPerToken != 0.000009 {
		t.Errorf("同步覆盖了手工配置: %+v", kept)
	}

	// 删除
	if err := svc.DeletePrice(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := priceRepo.GetByModel(ctx, "my-model"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("删除后仍可查到: %v", err)
	}
	// 不存在的 id 更新应给出业务错误
	if _, err := svc.UpdatePrice(ctx, 99999, &service.PriceInput{InputCostPerToken: 1}); err == nil {
		t.Error("不存在的 id 更新应报错")
	}
}

func TestSyncFailureKeepsStatus(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	src := testutil.NewStubPricingSource(sonnetEntry())
	src.Err = errors.New("网络不可达")
	svc := service.NewCostService(
		repository.NewModelPriceRepository(db),
		repository.NewRequestLogRepository(db),
		repository.NewBillingSettingsRepository(db),
		src, service.CostOptions{Enabled: true},
	)
	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("源失败时 Sync 应报错")
	}
	st := svc.Status(context.Background())
	if st.LastError == "" {
		t.Error("失败原因应记录到状态里")
	}
	if st.LastSyncAt != nil {
		t.Error("失败不应更新上次同步时间")
	}
}

// ---- 费用重算 ----

func TestRecompute(t *testing.T) {
	svc, _, logRepo := newCostService(t, sonnetEntry())
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	logs := []model.RequestLog{
		// 新口径：网关已写入 usage_style
		{Model: "claude-sonnet-4", Protocol: model.ProtocolMessages, ForwardMode: model.ForwardNativePassthrough,
			ChannelID: 1, UsageStyle: model.UsageStyleAnthropic, PromptTokens: 100, CompletionTokens: 20,
			CachedTokens: 40, CacheWriteTokens: 7, CreatedAt: now},
		// 存量行：无 usage_style、无费用（靠 cache_write_tokens 推断为 anthropic）
		{Model: "claude-sonnet-4", Protocol: model.ProtocolMessages, ForwardMode: model.ForwardNativePassthrough,
			ChannelID: 1, PromptTokens: 100, CompletionTokens: 20, CachedTokens: 40, CacheWriteTokens: 7,
			CreatedAt: now.Add(-time.Minute)},
		// 未定价模型
		{Model: "mystery", Protocol: model.ProtocolChatCompletions, ForwardMode: model.ForwardNativePassthrough,
			ChannelID: 1, PromptTokens: 100, CreatedAt: now},
	}
	for i := range logs {
		if err := logRepo.Create(ctx, &logs[i]); err != nil {
			t.Fatal(err)
		}
	}

	want := 100*rateIn + 40*rateRead + 7*rateWrite + 20*rateOut
	res, err := svc.Recompute(ctx, time.Time{}, time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Scanned != 3 {
		t.Errorf("扫描 = %d, want 3", res.Scanned)
	}
	// 三条都要更新：两条补费用，未定价那条补口径（费用仍为 0）
	if res.Updated != 3 {
		t.Errorf("更新 = %d, want 3", res.Updated)
	}

	for i := range logs {
		got, err := logRepo.Get(ctx, logs[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Model == "mystery" {
			if got.CostUSD != 0 {
				t.Errorf("未定价模型费用 = %v, want 0", got.CostUSD)
			}
			if got.UsageStyle != model.UsageStyleOpenAI {
				t.Errorf("未定价模型口径 = %q, want openai", got.UsageStyle)
			}
			continue
		}
		approx(t, got.CostUSD, want, "重算费用")
		if got.UsageStyle != model.UsageStyleAnthropic {
			t.Errorf("重算口径 = %q, want anthropic", got.UsageStyle)
		}
	}

	// 幂等：再跑一次不应产生写入
	res2, err := svc.Recompute(ctx, time.Time{}, time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Updated != 0 {
		t.Errorf("重复重算更新 = %d, want 0（幂等）", res2.Updated)
	}
}

// ---- 计费设置 ----

func TestBillingSettings(t *testing.T) {
	svc, _, _ := newCostService(t, sonnetEntry())
	ctx := context.Background()

	// EnsureDefaults 种子化：默认 USD + 配置里的汇率
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	cur, err := svc.Billing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cur.DisplayCurrency != model.CurrencyUSD || cur.USDRate != 7.2 {
		t.Errorf("初始设置 = %+v, want USD/7.2", cur)
	}

	// 切 CNY 但缺汇率应报错
	if _, err := svc.UpdateBilling(ctx, &service.UpdateBillingInput{DisplayCurrency: "CNY"}); err == nil {
		t.Error("CNY 缺汇率应报错")
	}
	if _, err := svc.UpdateBilling(ctx, &service.UpdateBillingInput{DisplayCurrency: "JPY"}); err == nil {
		t.Error("不支持的币种应报错")
	}
	if _, err := svc.UpdateBilling(ctx, &service.UpdateBillingInput{DisplayCurrency: "USD", MonthlyBudgetUSD: -1}); err == nil {
		t.Error("负预算应报错")
	}

	saved, err := svc.UpdateBilling(ctx, &service.UpdateBillingInput{
		DisplayCurrency: "CNY", USDRate: 7.15, MonthlyBudgetUSD: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.DisplayCurrency != model.CurrencyCNY || saved.USDRate != 7.15 || saved.MonthlyBudgetUSD != 100 {
		t.Errorf("保存结果 = %+v", saved)
	}
	again, err := svc.Billing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.DisplayCurrency != model.CurrencyCNY || again.MonthlyBudgetUSD != 100 {
		t.Errorf("回读 = %+v", again)
	}
}

// ---- 未定价模型 ----

func TestUnpricedModels(t *testing.T) {
	// 价格键带厂商前缀：日志里的裸名只能靠别名规则命中（zai/glm-5.3-flash -> glm-5.3-flash）。
	aliasEntry := provider.ModelPriceEntry{
		Model:             "zai/glm-5.3-flash",
		InputCostPerToken: rateIn, OutputCostPerToken: rateOut,
	}
	svc, _, logRepo := newCostService(t, sonnetEntry(), aliasEntry)
	ctx := context.Background()
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	for _, l := range []model.RequestLog{
		{Model: "claude-sonnet-4", Protocol: model.ProtocolMessages, PromptTokens: 10, TotalTokens: 10, CreatedAt: time.Now()},
		// 仅靠别名命中，不应报未定价：这正是修复前被误报的场景。
		{Model: "glm-5.3-flash", Protocol: model.ProtocolMessages, PromptTokens: 20, TotalTokens: 20, CreatedAt: time.Now()},
		{Model: "mystery", Protocol: model.ProtocolMessages, PromptTokens: 90, TotalTokens: 90, CreatedAt: time.Now()},
	} {
		if err := logRepo.Create(ctx, &l); err != nil {
			t.Fatal(err)
		}
	}
	items, err := svc.UnpricedModels(ctx, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Model != "mystery" || items[0].TotalTokens != 90 {
		t.Errorf("未定价模型 = %+v, want [mystery]", items)
	}
}

// ---- 时段价与币种 ----

const perMillion = 1_000_000.0

// dsPriceInput 构造 DeepSeek 式手工价入参：人民币、每百万报价（高峰 输入2/输出8，空闲半价 1/4）。
func dsPriceInput() *service.PriceInput {
	return &service.PriceInput{
		Model:    "deepseek-flash",
		Currency: model.CurrencyCNY,
		// 后端存单 token，这里把每百万报价折算回去
		InputCostPerToken:         2 / perMillion,
		OutputCostPerToken:        8 / perMillion,
		PeakWindow:                deepseekPeakWindow,
		OffPeakInputCostPerToken:  1 / perMillion,
		OffPeakOutputCostPerToken: 4 / perMillion,
	}
}

// createManualPrice 走手工价路径落库（同步路径会强制 USD 且抹掉时段价，不能用于本组测试）。
func createManualPrice(t *testing.T, svc *service.CostService, in *service.PriceInput) {
	t.Helper()
	if _, err := svc.CreatePrice(context.Background(), in); err != nil {
		t.Fatalf("创建手工价: %v", err)
	}
}

// 高峰用高峰价、空闲用空闲价；命中时段同时写入 PricePeriod。
func TestCostServicePeakAndOffPeak(t *testing.T) {
	svc, _, _ := newCostService(t)
	createManualPrice(t, svc, dsPriceInput())

	// 每百万人民币 -> 单 token
	const inPeak, outPeak = 2 / perMillion, 8 / perMillion
	const inOff, outOff = 1 / perMillion, 4 / perMillion

	cases := []struct {
		name       string
		at         time.Time
		wantPeriod model.PricePeriod
		wantCNY    float64
	}{
		{"工作日高峰 10:00", atPeak, model.PricePeriodPeak, 100*inPeak + 20*outPeak},
		{"工作日午休 13:00", atLunchBreak, model.PricePeriodOffPeak, 100*inOff + 20*outOff},
		{"周末 10:00", atWeekend, model.PricePeriodOffPeak, 100*inOff + 20*outOff},
		{"工作日夜间 20:00", atEvening, model.PricePeriodOffPeak, 100*inOff + 20*outOff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log := &model.RequestLog{
				Model: "deepseek-flash", PromptTokens: 100, CompletionTokens: 20,
				CreatedAt: c.at,
			}
			got := svc.Cost(log, model.UsageStyleOpenAI)
			approx(t, got, c.wantCNY/testUSDRate, c.name+" 费用（人民币按汇率折美元）")
			if log.PricePeriod != c.wantPeriod {
				t.Errorf("%s 时段 = %q, want %q", c.name, log.PricePeriod, c.wantPeriod)
			}
		})
	}
}

// 未配置空闲费率（0）时，空闲时段回退高峰费率，但时段仍记录为 off_peak。
func TestCostServiceOffPeakFallsBackToPeakRates(t *testing.T) {
	svc, _, _ := newCostService(t)
	in := dsPriceInput()
	in.OffPeakInputCostPerToken = 0
	in.OffPeakOutputCostPerToken = 0
	createManualPrice(t, svc, in)

	const inPeak, outPeak = 2 / perMillion, 8 / perMillion
	log := &model.RequestLog{
		Model: "deepseek-flash", PromptTokens: 100, CompletionTokens: 20,
		CreatedAt: atEvening,
	}
	got := svc.Cost(log, model.UsageStyleOpenAI)
	approx(t, got, (100*inPeak+20*outPeak)/testUSDRate, "空闲回退高峰费率")
	if log.PricePeriod != model.PricePeriodOffPeak {
		t.Errorf("即使回退费率，时段仍应为 off_peak, got %q", log.PricePeriod)
	}
}

// 无时段配置的模型：PricePeriod 为空，且任何时刻费用一致。
func TestCostServiceNoPeakWindowLeavesPeriodEmpty(t *testing.T) {
	svc, _, _ := newCostService(t, sonnetEntry())
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("同步价格表: %v", err)
	}
	for _, at := range []time.Time{atPeak, atWeekend} {
		log := &model.RequestLog{
			Model: "claude-sonnet-4", PromptTokens: 100, CompletionTokens: 20, CreatedAt: at,
		}
		svc.Cost(log, model.UsageStyleAnthropic)
		if log.PricePeriod != "" {
			t.Errorf("未配置时段规则时 PricePeriod = %q, want 空", log.PricePeriod)
		}
	}
}

// 人民币行在汇率为 0 时记 0（不产出量纲错误的费用）；填好汇率后立即正常折算。
func TestCostServiceCNYWithoutRate(t *testing.T) {
	db := testutil.NewMemoryDB(t)
	svc := service.NewCostService(
		repository.NewModelPriceRepository(db),
		repository.NewRequestLogRepository(db),
		repository.NewBillingSettingsRepository(db),
		testutil.NewStubPricingSource(),
		service.CostOptions{Enabled: true, SyncInterval: time.Hour,
			// 汇率 0：模拟「人民币价已配、汇率未填」
			InitialBilling: model.BillingSettings{DisplayCurrency: model.CurrencyUSD, USDRate: 0}},
	)
	ctx := context.Background()
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	createManualPrice(t, svc, dsPriceInput())

	log := &model.RequestLog{Model: "deepseek-flash", PromptTokens: 100, CompletionTokens: 20, CreatedAt: atPeak}
	if got := svc.Cost(log, model.UsageStyleOpenAI); got != 0 {
		t.Errorf("汇率未配置时人民币行费用 = %v, want 0", got)
	}
	// 时段仍应记录，便于事后重算
	if log.PricePeriod != model.PricePeriodPeak {
		t.Errorf("时段 = %q, want peak", log.PricePeriod)
	}

	// 填入汇率后立即生效（UpdateBilling 必须失效汇率缓存，否则要等一个 TTL）
	if _, err := svc.UpdateBilling(ctx, &service.UpdateBillingInput{
		DisplayCurrency: model.CurrencyUSD, USDRate: testUSDRate,
	}); err != nil {
		t.Fatalf("UpdateBilling: %v", err)
	}
	const inPeak, outPeak = 2 / perMillion, 8 / perMillion
	log2 := &model.RequestLog{Model: "deepseek-flash", PromptTokens: 100, CompletionTokens: 20, CreatedAt: atPeak}
	approx(t, svc.Cost(log2, model.UsageStyleOpenAI), (100*inPeak+20*outPeak)/testUSDRate,
		"填汇率后按汇率折算")
}

// 时段与长上下文分档叠加：空闲取空闲基础费率，超阈值整单覆盖。
func TestCostServicePeakWithThresholdTier(t *testing.T) {
	p := priceModel(provider.ModelPriceEntry{
		Model:                     "m",
		InputCostPerToken:         2 / perMillion,
		OutputCostPerToken:        8 / perMillion,
		PeakWindow:                model.MustPeakWindow(deepseekPeakWindow),
		OffPeakInputCostPerToken:  1 / perMillion,
		OffPeakOutputCostPerToken: 4 / perMillion,
		ThresholdTokens:           1000,
		InputCostAbovePerToken:    10 / perMillion,
		OutputCostAbovePerToken:   40 / perMillion,
	})
	// 空闲 + 超阈值：输入侧取超阈值费率
	_, _, in, _ := p.Rates(atEvening, 2000)
	approx(t, in, 10/perMillion, "空闲+超阈值应取分档输入费率")
	// 空闲但未超阈值：取空闲费率
	_, _, in2, _ := p.Rates(atEvening, 500)
	approx(t, in2, 1/perMillion, "空闲未超阈值应取空闲输入费率")
	// 高峰 + 超阈值：同样取分档费率
	_, _, in3, _ := p.Rates(atPeak, 2000)
	approx(t, in3, 10/perMillion, "高峰+超阈值应取分档输入费率")
}

// PriceInput 校验：非法币种与非法时段规则必须被拒。
func TestPriceInputValidation(t *testing.T) {
	svc, _, _ := newCostService(t)
	ctx := context.Background()

	base := func() *service.PriceInput {
		return &service.PriceInput{Model: "m", InputCostPerToken: 1e-6, PeakWindow: deepseekPeakWindow}
	}

	in := base()
	in.Currency = "EUR"
	if _, err := svc.CreatePrice(ctx, in); err == nil {
		t.Error("非法币种应报错")
	}

	in = base()
	in.PeakWindow = "1-5" // 缺时段段
	if _, err := svc.CreatePrice(ctx, in); err == nil {
		t.Error("非法时段规则应报错")
	}

	in = base()
	in.OffPeakInputCostPerToken = -1
	if _, err := svc.CreatePrice(ctx, in); err == nil {
		t.Error("负的空闲费率应报错")
	}

	// 合法：人民币 + 时段 + 空闲费率的完整往返
	in = base()
	in.Currency = "CNY"
	in.OffPeakInputCostPerToken = 5e-7
	created, err := svc.CreatePrice(ctx, in)
	if err != nil {
		t.Fatalf("合法人民币时段价应创建成功: %v", err)
	}
	if created.Currency != model.CurrencyCNY || !created.PeakWindow.Valid() {
		t.Errorf("落库字段不符: currency=%q window=%q", created.Currency, created.PeakWindow.String())
	}
	if created.OffPeakInputCostPerToken != 5e-7 {
		t.Errorf("空闲费率未落库: %v", created.OffPeakInputCostPerToken)
	}
}

// 同步写入的行必须是 USD 且不带时段价/空闲费率（公开价格表没有这些信息）。
func TestSyncForcesUSDFlat(t *testing.T) {
	// 桩源刻意带上人民币与时段价：同步后必须被抹平。
	e := provider.ModelPriceEntry{
		Model: "deepseek-flash", Currency: model.CurrencyCNY,
		InputCostPerToken: 2 / perMillion, OutputCostPerToken: 8 / perMillion,
		PeakWindow:               model.MustPeakWindow(deepseekPeakWindow),
		OffPeakInputCostPerToken: 1 / perMillion,
	}
	svc, priceRepo, _ := newCostService(t, e)
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("同步: %v", err)
	}
	got, err := priceRepo.GetByModel(context.Background(), "deepseek-flash")
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if got.Currency != model.CurrencyUSD {
		t.Errorf("同步行币种 = %q, want USD", got.Currency)
	}
	if got.PeakWindow.Valid() {
		t.Errorf("同步行不应带时段规则, got %q", got.PeakWindow.String())
	}
	if got.OffPeakInputCostPerToken != 0 || got.OffPeakOutputCostPerToken != 0 {
		t.Errorf("同步行不应带空闲费率: %+v", got)
	}
}

// 手工人民币价在同步后不被覆盖，且保留时段配置（既有“手工价优先”保护的延伸）。
func TestManualCNYPriceSurvivesSync(t *testing.T) {
	svc, _, _ := newCostService(t, provider.ModelPriceEntry{
		Model: "deepseek-flash", InputCostPerToken: 9e-6, OutputCostPerToken: 9e-6,
	})
	createManualPrice(t, svc, dsPriceInput())

	res, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("同步: %v", err)
	}
	if res.SkippedManual != 1 {
		t.Errorf("skipped_manual = %d, want 1", res.SkippedManual)
	}
	// 同步后仍是人民币手工价、仍带时段规则
	log := &model.RequestLog{Model: "deepseek-flash", PromptTokens: 100, CompletionTokens: 20, CreatedAt: atPeak}
	const inPeak, outPeak = 2 / perMillion, 8 / perMillion
	approx(t, svc.Cost(log, model.UsageStyleOpenAI), (100*inPeak+20*outPeak)/testUSDRate,
		"同步后手工人民币价仍生效")
	if log.PricePeriod != model.PricePeriodPeak {
		t.Errorf("同步后时段规则丢失: %q", log.PricePeriod)
	}
}
