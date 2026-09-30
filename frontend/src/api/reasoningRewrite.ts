import { request, type PageData } from './http'

/** 思维档位改写规则：按上游模型把客户端的思维档位翻译成 thinking 开关（enabled/disabled）。 */
export interface ReasoningMapping {
  /** 客户端档位（none/minimal/low/medium/high/xhigh/max，归一化小写）。 */
  level: string
  /** thinking 开关目标。 */
  target: 'enabled' | 'disabled'
}

export interface ReasoningRewrite {
  id: number
  model: string
  mapping: ReasoningMapping[]
  /** 客户端未带档位时注入的开关（空 = 不注入）。 */
  missing: string
  status: 0 | 1
  remark: string
  created_at: string
  updated_at: string
}

export interface ReasoningRewriteInput {
  model: string
  mapping: ReasoningMapping[]
  missing?: string
  status?: 0 | 1
  remark?: string
}

/** 预置档位集合。 */
export const REASONING_TIERS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const

export const reasoningRewriteApi = {
  list: (params?: { page?: number; per_page?: number; q?: string }) =>
    request<PageData<ReasoningRewrite>>({ url: '/api/v1/reasoning-rewrites', method: 'GET', params }),
  create: (input: ReasoningRewriteInput) =>
    request<ReasoningRewrite>({ url: '/api/v1/reasoning-rewrites', method: 'POST', data: input }),
  update: (id: number, input: ReasoningRewriteInput) =>
    request<ReasoningRewrite>({ url: `/api/v1/reasoning-rewrites/${id}`, method: 'PUT', data: input }),
  remove: (id: number) => request<null>({ url: `/api/v1/reasoning-rewrites/${id}`, method: 'DELETE' }),
}
