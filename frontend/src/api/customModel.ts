import { request, type PageData } from './http'

/** 自定义模型组：把一组相似的真实模型聚合到同一个对外模型 ID 下，网关按成员优先级自动路由。 */
export interface ModelMember {
  model: string
  priority?: number
}

export interface CustomModel {
  id: number
  name: string
  members: ModelMember[]
  status: 0 | 1
  remark: string
  created_at: string
  updated_at: string
}

export interface CustomModelInput {
  name: string
  members: ModelMember[]
  status?: 0 | 1
  remark?: string
}

export const customModelApi = {
  list: (params?: { page?: number; per_page?: number }) =>
    request<PageData<CustomModel>>({ url: '/api/v1/custom-models', method: 'GET', params }),
  create: (input: CustomModelInput) =>
    request<CustomModel>({ url: '/api/v1/custom-models', method: 'POST', data: input }),
  update: (id: number, input: CustomModelInput) =>
    request<CustomModel>({ url: `/api/v1/custom-models/${id}`, method: 'PUT', data: input }),
  remove: (id: number) => request<null>({ url: `/api/v1/custom-models/${id}`, method: 'DELETE' }),
}
