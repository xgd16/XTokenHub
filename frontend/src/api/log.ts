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
  stream?: boolean
  error_only?: boolean
  hours?: number
}

export const logApi = {
  list: (params: LogQuery) => request<PageData<RequestLog>>({ url: '/api/v1/logs', method: 'GET', params }),
}
