import { request } from './http'

/** 模型价格行（后端 model.ModelPrice，费率单位为其 currency 对应的单 token 金额）。 */
export interface ModelPrice {
  id: number
  model: string
  /** 费率币种：USD | CNY。CNY 行按汇率折成 USD 计费，cost_usd 仍是 USD。 */
  currency: string
  input_cost_per_token: number
  output_cost_per_token: number
  cache_read_cost_per_token: number
  cache_write_cost_per_token: number
  /**
   * 高峰时段规则（北京时间），如 `1-5;09:00-12:00,14:00-18:00`；空 = 不启用时段价。
   * off_peak_* 为对应空闲时段费率；未配置（0）时回退高峰费率。
   */
  peak_window: string
  off_peak_input_cost_per_token: number
  off_peak_output_cost_per_token: number
  off_peak_cache_read_cost_per_token: number
  off_peak_cache_write_cost_per_token: number
  threshold_tokens: number
  input_cost_above_per_token: number
  output_cost_above_per_token: number
  cache_read_cost_above_per_token: number
  cache_write_cost_above_per_token: number
  /** synced（同步自动写入） | manual（手工配置，同步不覆盖）。 */
  source: string
  provider: string
  synced_at?: string
  created_at: string
  updated_at: string
}

/** DeepSeek 官方错峰规则：工作日 9:00-12:00、14:00-18:00 为高峰（北京时间）。 */
export const DEEPSEEK_PEAK_WINDOW = '1-5;09:00-12:00,14:00-18:00'

/** 从价格表同步到本地的结果。 */
export interface SyncResult {
  fetched: number
  written: number
  skipped_manual: number
  synced_at: string
  source: string
}

/** 价格表同步状态。 */
export interface SyncStatus {
  enabled: boolean
  auto_sync: boolean
  source_url: string
  interval_hours: number
  last_sync_at?: string
  last_error?: string
  last_synced_count: number
  price_count: number
}

/** 有用量但价格表未覆盖的模型。 */
export interface UnpricedModel {
  model: string
  requests: number
  total_tokens: number
}

/** 计费展示设置。 */
export interface BillingSettings {
  display_currency: string
  usd_cny_rate: number
  monthly_budget_usd: number
}

/** 价格表分页响应：附带同步状态、未定价模型与计费设置，避免设置页多次往返。 */
export interface PriceListResponse {
  items: ModelPrice[]
  total: number
  page: number
  per_page: number
  status: SyncStatus
  unpriced: UnpricedModel[]
  billing: BillingSettings | null
}

/** 价格录入/编辑入参。 */
export type PriceInput = Omit<
  ModelPrice,
  'id' | 'source' | 'provider' | 'synced_at' | 'created_at' | 'updated_at'
>

export const pricingApi = {
  listPrices: (params: { q?: string; used_only?: boolean; page?: number; per_page?: number }) =>
    request<PriceListResponse>({ url: '/api/v1/settings/prices', method: 'GET', params }),
  createPrice: (data: Partial<PriceInput>) =>
    request<ModelPrice>({ url: '/api/v1/settings/prices', method: 'POST', data }),
  updatePrice: (id: number, data: Partial<PriceInput>) =>
    request<ModelPrice>({ url: `/api/v1/settings/prices/${id}`, method: 'PUT', data }),
  deletePrice: (id: number) =>
    request<null>({ url: `/api/v1/settings/prices/${id}`, method: 'DELETE' }),
  sync: () => request<SyncResult>({ url: '/api/v1/settings/prices/sync', method: 'POST' }),
  getBilling: () => request<BillingSettings>({ url: '/api/v1/settings/billing', method: 'GET' }),
  updateBilling: (data: BillingSettings) =>
    request<BillingSettings>({ url: '/api/v1/settings/billing', method: 'PUT', data }),
}

/** 花费预测结果。projected_usd 为 null 表示样本不足（见 reason）。 */
export interface CostForecast {
  period: 'today' | 'month'
  period_start: string
  period_end: string
  spent_usd: number
  projected_usd: number | null
  burn_per_hour_usd: number
  daily_avg_usd: number
  /** run_rate（近 7 日日均） | linear（本周期线性外推）。 */
  basis: string
  confidence: 'low' | 'medium' | 'high'
  reason?: string
  budget_usd: number
  projected_exceeded_date?: string
}

/** 费用重算结果。 */
export interface RecomputeResult {
  scanned: number
  updated: number
  from: string
  to: string
}

export const costApi = {
  forecast: (period: 'today' | 'month') =>
    request<CostForecast>({ url: '/api/v1/stats/cost/forecast', method: 'GET', params: { period } }),
  unpriced: (hours = 720) =>
    request<UnpricedModel[]>({ url: '/api/v1/stats/cost/unpriced', method: 'GET', params: { hours } }),
  /** 重算历史费用；onlyMissing 为 true 时只补算尚无费用的行。 */
  recompute: (data: { from?: number; to?: number; only_missing?: boolean }) =>
    request<RecomputeResult>({ url: '/api/v1/stats/cost/recompute', method: 'POST', data }),
}
