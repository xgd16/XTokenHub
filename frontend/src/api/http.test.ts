import { afterEach, describe, expect, it } from 'vitest'
import type { AxiosAdapter, AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import { http, request } from './http'

/** 用自定义 adapter 替换传输层，走真实的拦截器 + request() 解包链路。 */
function useAdapter(adapter: AxiosAdapter) {
  const original = http.defaults.adapter
  http.defaults.adapter = adapter
  return () => {
    http.defaults.adapter = original
  }
}

/** 构造一个「服务端已响应」的 axios 错误（含 response.data）。 */
function respondError(status: number, data: unknown): AxiosAdapter {
  return async (config) => {
    const response: AxiosResponse = {
      data,
      status,
      statusText: String(status),
      headers: {},
      config: config as InternalAxiosRequestConfig,
    }
    const err = Object.assign(new Error(`Request failed with status code ${status}`), {
      isAxiosError: true,
      response,
      config,
    })
    throw err
  }
}

describe('http 错误信息提取', () => {
  let restore: (() => void) | undefined
  afterEach(() => {
    restore?.()
    restore = undefined
  })

  it('后端 4xx + {code,message} 时透出 message，而非 axios 兜底文案', async () => {
    restore = useAdapter(respondError(400, { code: 1001, message: '渠道名已存在', data: null }))
    await expect(request({ url: '/api/v1/channels' })).rejects.toThrow('渠道名已存在')
  })

  it('有响应但无 message 时带出 HTTP 状态码', async () => {
    restore = useAdapter(respondError(500, null))
    await expect(request({ url: '/api/v1/channels' })).rejects.toThrow('请求失败（HTTP 500）')
  })

  it('无响应（网络层失败）时给出网络异常提示', async () => {
    restore = useAdapter(async () => {
      throw Object.assign(new Error('Network Error'), { isAxiosError: true })
    })
    await expect(request({ url: '/api/v1/channels' })).rejects.toThrow('网络异常，请检查后端服务')
  })

  it('HTTP 200 但业务 code !== 0 时抛出 message', async () => {
    restore = useAdapter(async (config) => ({
      data: { code: 1001, message: '参数错误', data: null },
      status: 200,
      statusText: 'OK',
      headers: {},
      config: config as InternalAxiosRequestConfig,
    }))
    await expect(request({ url: '/api/v1/channels' })).rejects.toThrow('参数错误')
  })

  it('业务成功时解包 data', async () => {
    restore = useAdapter(async (config) => ({
      data: { code: 0, message: 'ok', data: { id: 7 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config: config as InternalAxiosRequestConfig,
    }))
    await expect(request<{ id: number }>({ url: '/api/v1/channels/7' })).resolves.toEqual({ id: 7 })
  })
})
