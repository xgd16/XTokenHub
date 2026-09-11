package model

import "time"

// 展示币种。金额一律以 USD 存储与计算，币种只影响展示。
const (
	CurrencyUSD = "USD"
	CurrencyCNY = "CNY"
)

// BillingSettings 计费展示设置（单行表，ID 固定为 1）。
type BillingSettings struct {
	ID int64 `gorm:"primaryKey" json:"id"`

	// DisplayCurrency 默认展示币种：USD | CNY。
	DisplayCurrency string `gorm:"size:8;not null;default:USD" json:"display_currency"`
	// USDRate USD -> CNY 汇率，手工配置（不引入外部汇率接口）。
	USDRate float64 `json:"usd_cny_rate"`
	// MonthlyBudgetUSD 月度预算上限，0 = 不设预算（仅用于预测提示，不拦截请求）。
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`

	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (BillingSettings) TableName() string {
	return "billing_settings"
}
