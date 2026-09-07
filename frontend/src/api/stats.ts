import { request } from './http'

export interface Summary {
  total_requests: number
  success_requests: number
  error_requests: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cached_tokens: number
  cache_hit_rate: number
  avg_duration_ms: number
  native_ratio: number
}

export interface TrendPoint {
  date: string
  ts?: number // 桶起点 Unix 秒（分钟/小时分桶时返回）
  requests: number
  total_tokens: number
  error_requests: number
}

export type TrendBucket = 'minute' | 'hour' | 'day'

export interface GroupStat {
  name: string
  requests: number
  total_tokens: number
  cached_tokens: number
  cache_rate: number
  avg_ms: number
}

/** 模型在单个时间窗（1h/24h/7d/30d）内的使用量。 */
export interface ModelWindow {
  requests: number
  total_tokens: number
  cached_tokens: number
}

/** 单个模型的多时间窗使用量（目录含未调用模型，各窗口可为零值）。 */
export interface ModelUsage {
  model: string
  w_1h: ModelWindow
  w_24h: ModelWindow
  w_7d: ModelWindow
  w_30d: ModelWindow
}

export const statsApi = {
  summary: (hours = 24, since?: number) =>
    request<Summary>({ url: '/api/v1/stats/summary', method: 'GET', params: since ? { hours, since } : { hours } }),
  trend: (hours = 168, bucket: TrendBucket = 'day') =>
    request<TrendPoint[]>({ url: '/api/v1/stats/trend', method: 'GET', params: { hours, bucket } }),
  /** 按日趋势（days 可到 366，供热力图/长区间）。 */
  trendByDay: (days = 7) =>
    request<TrendPoint[]>({ url: '/api/v1/stats/trend', method: 'GET', params: { days } }),
  byModel: (hours = 24, since?: number) =>
    request<GroupStat[]>({ url: '/api/v1/stats/by-model', method: 'GET', params: since ? { hours, since } : { hours } }),
  byChannel: (hours = 24) => request<GroupStat[]>({ url: '/api/v1/stats/by-channel', method: 'GET', params: { hours } }),
  byKey: (hours = 24) => request<GroupStat[]>({ url: '/api/v1/stats/by-key', method: 'GET', params: { hours } }),
}

/** 模型目录与用量：全部模型 + 多时间窗使用量。 */
export const modelsApi = {
  usage: () => request<ModelUsage[]>({ url: '/api/v1/models/usage', method: 'GET' }),
}

/** 全历史累计统计（lifetime）。 */
export interface Lifetime {
  total_tokens: number
  peak_day_tokens: number
  peak_day: string
  max_duration_ms: number
  current_streak: number
  max_streak: number
  active_days: number
}

/** 单日单模型 token 用量。 */
export interface ModelDayPoint {
  date: string
  model: string
  total_tokens: number
}

/** 当天零点（本地时区）的 Unix 秒，供「当天」口径统计。 */
export function todaySince(now: Date = new Date()): number {
  return Math.floor(new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() / 1000)
}

export const lifetimeApi = {
  get: () => request<Lifetime>({ url: '/api/v1/stats/lifetime', method: 'GET' }),
  trendByModel: (days = 7) =>
    request<ModelDayPoint[]>({ url: '/api/v1/stats/trend-by-model', method: 'GET', params: { days } }),
}
