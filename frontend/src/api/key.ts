import { request, type PageData } from './http'

/** 网关调用密钥：标识调用方（agent/脚本），用于鉴权与按调用方统计。 */
export interface APIKey {
  id: number
  name: string
  key: string
  status: 0 | 1
  remark: string
  created_at: string
  updated_at: string
}

export interface KeyInput {
  name: string
  status?: 0 | 1
  remark?: string
}

export const keyApi = {
  list: (params?: { page?: number; per_page?: number }) =>
    request<PageData<APIKey>>({ url: '/api/v1/keys', method: 'GET', params }),
  create: (input: KeyInput) =>
    request<APIKey>({ url: '/api/v1/keys', method: 'POST', data: input }),
  update: (id: number, input: KeyInput) =>
    request<APIKey>({ url: `/api/v1/keys/${id}`, method: 'PUT', data: input }),
  remove: (id: number) => request<null>({ url: `/api/v1/keys/${id}`, method: 'DELETE' }),
}

/** 密钥脱敏展示：sk-xt-abcd…wxyz（保留前 9 位与后 4 位）。 */
export function maskKey(key: string): string {
  if (key.length <= 16) return key
  return `${key.slice(0, 9)}…${key.slice(-4)}`
}
