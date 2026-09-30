import { request } from './http'
import type { ChannelBalance } from './channel'

/**
 * MiMo 网页余额凭证（后端 model.MimoSettings 单行）。
 * 官方未开放余额 API，保存网页控制台的会话 Cookie，由渠道余额查询按
 * Cookie 鉴权重放平台接口（最小集 userId + api-platform_serviceToken）。
 */
export interface MimoSettings {
  id: number
  cookie: string
  updated_at: string
}

/** 凭证验证结果：保存后立即拉一次渠道余额得到。 */
export interface MimoVerifyResult {
  ok: boolean
  /** 成功为余额摘要，失败为原因。 */
  text: string
}

export const mimoSettingsApi = {
  /** 读取当前凭证（明文回显，与渠道 api_key 同一安全姿态）。 */
  get: () => request<MimoSettings>({ url: '/api/v1/settings/mimo', method: 'GET' }),
  /** 保存凭证（后端保存后立即失效余额缓存，随后可重新拉取验证）。 */
  update: (cookie: string) =>
    request<MimoSettings>({ url: '/api/v1/settings/mimo', method: 'PUT', data: { cookie } }),
  /** 重拉渠道余额（验证用：取 provider=mimo 且 supported 的条目）。 */
  balances: () => request<{ items: ChannelBalance[] }>({ url: '/api/v1/channels/balances', method: 'GET' }),
}
