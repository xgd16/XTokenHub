import { request, type PageData } from './http'

export type Protocol = 'chat_completions' | 'responses' | 'messages'
export type ProviderType = 'openai_compatible' | 'anthropic'
export type ForwardMode = 'native_passthrough' | 'converted'

export const PROTOCOLS: { value: Protocol; label: string; path: string }[] = [
  { value: 'chat_completions', label: 'Chat Completions', path: '/v1/chat/completions' },
  { value: 'responses', label: 'Responses', path: '/v1/responses' },
  { value: 'messages', label: 'Messages', path: '/v1/messages' },
]

export const PROVIDERS: { value: ProviderType; label: string }[] = [
  { value: 'openai_compatible', label: 'OpenAI 兼容（Bearer）' },
  { value: 'anthropic', label: 'Anthropic 兼容（x-api-key）' },
]

/** 新建渠道默认值：按 BaseURL 自动推断接口风格。 */
export const PROVIDER_AUTO = 'auto'

export const protocolLabel = (p: string) => PROTOCOLS.find((x) => x.value === p)?.label ?? p

export interface Channel {
  id: number
  name: string
  provider: ProviderType
  base_url: string
  api_key: string
  models: string
  native_protocols: string
  priority: number
  weight: number
  status: 0 | 1
  remark: string
  last_probe_at: string | null
  probe_result: string
  created_at: string
  updated_at: string
}

export interface ChannelInput {
  name: string
  /** 省略时后端按 base_url 自动推断接口风格。 */
  provider?: ProviderType
  base_url: string
  api_key: string
  models?: string[]
  native_protocols?: Protocol[]
  priority?: number
  weight?: number
  status?: 0 | 1
  remark?: string
}

export interface ProbeItem {
  protocol: Protocol
  ok: boolean
  status: number
  detail: string
}

export interface ProbeReport {
  probe_model: string
  native_protocols: Protocol[]
  items: ProbeItem[]
  probed_at: string
}

export interface LookupModelsInput {
  /** 省略时后端按 base_url 自动推断接口风格。 */
  provider?: ProviderType
  base_url: string
  api_key: string
}

/** 上游账户余额（当前支持 DeepSeek）。 */
export interface BalanceInfo {
  provider: 'deepseek'
  is_available: boolean
  currency: string
  total: number
  granted: number
  topped_up: number
  fetched_at: string
}

/** 单渠道余额查询结果；supported=false 表示该 BaseURL 无对应余额接口。 */
export interface ChannelBalance {
  channel_id: number
  channel_name: string
  provider: string
  supported: boolean
  ok: boolean
  balance?: BalanceInfo
  error?: string
  fetched_at?: string
}

export const channelApi = {  list: (params?: { page?: number; per_page?: number }) =>
    request<PageData<Channel>>({ url: '/api/v1/channels', method: 'GET', params }),
  get: (id: number) => request<Channel>({ url: `/api/v1/channels/${id}`, method: 'GET' }),
  create: (input: ChannelInput) =>
    request<Channel>({ url: '/api/v1/channels', method: 'POST', data: input }),
  update: (id: number, input: ChannelInput) =>
    request<Channel>({ url: `/api/v1/channels/${id}`, method: 'PUT', data: input }),
  remove: (id: number) => request<null>({ url: `/api/v1/channels/${id}`, method: 'DELETE' }),
  probe: (id: number, probeModel?: string) =>
    request<ProbeReport>({ url: `/api/v1/channels/${id}/probe`, method: 'POST', data: { model: probeModel } }),
  /** 用给定凭据拉取上游模型列表（渠道可尚未创建）。 */
  lookupModels: (input: LookupModelsInput) =>
    request<{ models: string[] }>({ url: '/api/v1/channels/lookup-models', method: 'POST', data: input }),
  /** 批量查询渠道上游账户余额（按 BaseURL 识别厂家，TTL 缓存）。 */
  balances: () =>
    request<{ items: ChannelBalance[] }>({ url: '/api/v1/channels/balances', method: 'GET' }),
}

export function parseCSV(csv: string): string[] {
  return csv ? csv.split(',').map((s) => s.trim()).filter(Boolean) : []
}

export function nativeProtocolsOf(ch: Channel): Protocol[] {
  return parseCSV(ch.native_protocols) as Protocol[]
}
