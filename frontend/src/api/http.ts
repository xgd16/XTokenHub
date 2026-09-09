import axios from 'axios'

/** 后端统一响应体 {code, message, data} */
export interface Envelope<T = unknown> {
  code: number
  message: string
  data: T
}

export const http = axios.create({
  baseURL: '/',
  timeout: 30_000,
})

// 统一解包：业务 code !== 0 视为失败
http.interceptors.response.use(
  (resp) => resp,
  (error) => {
    // 后端业务失败以 HTTP 4xx/5xx + {code,message} 返回，这里提取 message，
    // 否则页面只能拿到 axios 的 "Request failed with status code 400" 兜底文案。
    const body = error?.response?.data as { message?: unknown } | undefined
    if (body && typeof body.message === 'string' && body.message) {
      return Promise.reject(new Error(body.message))
    }
    if (error?.response) {
      return Promise.reject(new Error(`请求失败（HTTP ${error.response.status}）`))
    }
    return Promise.reject(new Error('网络异常，请检查后端服务'))
  },
)

/** 请求并解包 data；业务失败抛出 message。 */
export async function request<T>(config: Parameters<typeof http.request>[0]): Promise<T> {
  const resp = await http.request<Envelope<T>>(config)
  const env = resp.data
  if (env.code !== 0) {
    throw new Error(env.message || `业务错误 code=${env.code}`)
  }
  return env.data
}

/** 分页响应结构。 */
export interface PageData<T> {
  items: T[]
  total: number
  page: number
  per_page: number
}
