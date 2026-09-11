import { request, type PageData } from './http'

export interface RequestLog {
  id: number
  req_id?: number // 进程内请求序号：仅在实时推送里出现，进行中/完成配对用
  created_at: string
  protocol: string
  forward_mode: string
  channel_id: number
  channel_name: string
  key_id: number
  key_name: string
  model: string
  stream: boolean
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cached_tokens: number
  cache_write_tokens: number
  cache_hit_rate: number
  /** 本请求费用（USD）；未定价模型与失败请求为 0。 */
  cost_usd: number
  /** 计价所用的上游 usage 口径：openai | anthropic。 */
  usage_style?: string
  /** 计价命中的时段：peak | off_peak；模型未配置时段价时为空。 */
  price_period?: string
  duration_ms: number
  upstream_status: number
  client_ip: string
  user_agent: string
  session_id?: string // 调用方会话标识（入站 X-Session-Id 头），首页按会话聚合
  request_headers?: string
  error: string
}

export interface LogQuery {
  page?: number
  per_page?: number
  protocol?: string
  forward_mode?: string
  channel_id?: number
  key_id?: number
  model?: string
  session_id?: string
  stream?: boolean
  error_only?: boolean
  hours?: number
}

/** 手动清理结果。 */
export interface CleanupResult {
  deleted_rows: number
  duration_ms: number
  cutoff: string
  started_at: string
  done_at: string
  error?: string
}

/** 清理状态。 */
export interface CleanupStatus {
  enabled: boolean
  max_days: number
  interval_hours: number
  vacuum: boolean
  last_run_at?: string
  last_deleted_rows: number
  last_duration_ms: number
  last_error?: string
}

export const logApi = {
  list: (params: LogQuery) => request<PageData<RequestLog>>({ url: '/api/v1/logs', method: 'GET', params }),
  /** 手动触发一次清理。 */
  triggerCleanup: () => request<CleanupResult>({ url: '/api/v1/logs/cleanup', method: 'POST' }),
  /** 清理状态。 */
  cleanupStatus: () => request<CleanupStatus>({ url: '/api/v1/logs/cleanup', method: 'GET' }),
}
