package model

import "time"

// PriceSource 价格来源。
type PriceSource string

const (
	// PriceFromSynced 同步自公开价格表，每次同步会被整体替换。
	PriceFromSynced PriceSource = "synced"
	// PriceFromManual 手工配置，同步时保留（同名时手工值优先）。
	PriceFromManual PriceSource = "manual"
)

// IsCNY 是否为人民币计价行（空值按 USD 处理，兼容迁移前的历史行）。
func (p ModelPrice) IsCNY() bool { return p.CurrencyOrDefault() == CurrencyCNY }

// CurrencyOrDefault 返回生效币种：空值回退 USD。
func (p ModelPrice) CurrencyOrDefault() string {
	if p.Currency == CurrencyCNY {
		return CurrencyCNY
	}
	return CurrencyUSD
}

// ModelPrice 单模型计价费率。除 Currency 外其余费率均为**该币种**下的单个 token 金额
// （与公开价格表口径一致，不按百万 token 存储）。
//
// 长上下文只保留单档：ThresholdTokens > 0 且本次请求 prompt 超过该阈值时整单改用下面四个
// Above 费率，与 LiteLLM 的处理一致。公开价格表存在的多档 tiered_pricing 不支持。
//
// 时段价：PeakWindow 非空时，处于空闲时段的请求改用 OffPeak* 四个费率；空闲费率未配置
// （为 0）则回退高峰费率。OffPeak* 只作用于基础费率，不参与 Above 分档。
type ModelPrice struct {
	ID    int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Model string `gorm:"size:128;uniqueIndex;not null" json:"model"` // 上游真实模型名

	// Currency 费率币种：USD（默认）| CNY。CNY 行在计费时按汇率折算为 USD 后计算，
	// cost_usd 始终为 USD，聚合层无需感知币种。同步来源恒为 USD。
	Currency string `gorm:"size:8;not null;default:USD" json:"currency"`

	InputCostPerToken      float64 `json:"input_cost_per_token"`
	OutputCostPerToken     float64 `json:"output_cost_per_token"`
	CacheReadCostPerToken  float64 `json:"cache_read_cost_per_token"`
	CacheWriteCostPerToken float64 `json:"cache_write_cost_per_token"`

	// PeakWindow 高峰时段规则（如 `1-5;09:00-12:00,14:00-18:00`，北京时间）。
	// 为空表示不启用时段价。OffPeak* 为对应空闲时段费率（同 Currency）。
	PeakWindow                    PeakWindow `gorm:"type:text" json:"peak_window"`
	OffPeakInputCostPerToken      float64    `json:"off_peak_input_cost_per_token"`
	OffPeakOutputCostPerToken     float64    `json:"off_peak_output_cost_per_token"`
	OffPeakCacheReadCostPerToken  float64    `json:"off_peak_cache_read_cost_per_token"`
	OffPeakCacheWriteCostPerToken float64    `json:"off_peak_cache_write_cost_per_token"`

	// 超阈值分档（可选）。
	ThresholdTokens         int64   `json:"threshold_tokens"`
	InputCostAbovePerToken  float64 `json:"input_cost_above_per_token"`
	OutputCostAbovePerToken float64 `json:"output_cost_above_per_token"`
	CacheReadAbovePerToken  float64 `json:"cache_read_cost_above_per_token"`
	CacheWriteAbovePerToken float64 `json:"cache_write_cost_above_per_token"`

	// Source 为空视为 synced（迁移新增列的历史行为）。
	Source   PriceSource `gorm:"size:16;index" json:"source"`
	Provider string      `gorm:"size:64" json:"provider"` // 上游厂商标识，仅作展示与排查
	SyncedAt *time.Time  `json:"synced_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (ModelPrice) TableName() string {
	return "model_prices"
}

// Rates 返回本次请求适用的四类费率（原币种/token）。
//
// at 用于判定时段：命中空闲时段且该行配置了对应的 OffPeak 费率时改用空闲费率为基准。
// promptTokens 超过阈值时整单改用 Above 费率（与 LiteLLM 同口径）；未配置的 Above 值
// 保持基础费率。缓存费率缺失时回退为生效的输入费率——这也是 LiteLLM 自身的默认行为。
func (p ModelPrice) Rates(at time.Time, promptTokens int64) (in, out, cacheRead, cacheWrite float64) {
	in, out = p.InputCostPerToken, p.OutputCostPerToken
	cacheRead, cacheWrite = p.CacheReadCostPerToken, p.CacheWriteCostPerToken

	// 空闲时段：整体换用空闲那套基础费率；某个缓存费率缺失时留 0，
	// 由下面的回退逻辑兜到「本时段生效的输入费率」。
	if p.PeakWindow.Valid() && !p.PeakWindow.IsPeak(at) {
		if p.OffPeakInputCostPerToken > 0 {
			in = p.OffPeakInputCostPerToken
		}
		if p.OffPeakOutputCostPerToken > 0 {
			out = p.OffPeakOutputCostPerToken
		}
		cacheRead, cacheWrite = p.OffPeakCacheReadCostPerToken, p.OffPeakCacheWriteCostPerToken
	}

	if p.ThresholdTokens > 0 && promptTokens > p.ThresholdTokens {
		if p.InputCostAbovePerToken > 0 {
			in = p.InputCostAbovePerToken
		}
		if p.OutputCostAbovePerToken > 0 {
			out = p.OutputCostAbovePerToken
		}
		if p.CacheReadAbovePerToken > 0 {
			cacheRead = p.CacheReadAbovePerToken
		}
		if p.CacheWriteAbovePerToken > 0 {
			cacheWrite = p.CacheWriteAbovePerToken
		}
	}

	if cacheRead <= 0 {
		cacheRead = in
	}
	if cacheWrite <= 0 {
		cacheWrite = in
	}
	return in, out, cacheRead, cacheWrite
}

// IsManual 是否为手工配置（同步不覆盖）。
func (p ModelPrice) IsManual() bool { return p.Source == PriceFromManual }
