import { beforeEach, describe, expect, it, vi } from 'vitest'
import { channelApi, nativeProtocolsOf, parseCSV, type Channel } from './channel'
import { request } from './http'

vi.mock('./http', () => ({ request: vi.fn() }))

const mockRequest = vi.mocked(request)

const sampleChannel: Channel = {
  id: 1,
  name: 'openai-main',
  provider: 'openai_compatible',
  base_url: 'https://api.openai.com',
  api_key: 'sk-1',
  models: 'gpt-4o, claude-3-5-sonnet ,',
  native_protocols: 'chat_completions,messages',
  priority: 100,
  weight: 1,
  status: 1,
  remark: '',
  last_probe_at: null,
  probe_result: '',
  created_at: '',
  updated_at: '',
}

describe('parseCSV', () => {
  it('拆分并去除空白项', () => {
    expect(parseCSV('a, b ,c')).toEqual(['a', 'b', 'c'])
  })

  it('空串返回空数组', () => {
    expect(parseCSV('')).toEqual([])
  })
})

describe('nativeProtocolsOf', () => {
  it('解析渠道原生协议', () => {
    expect(nativeProtocolsOf(sampleChannel)).toEqual(['chat_completions', 'messages'])
  })
})

describe('channelApi.lookupModels', () => {
  beforeEach(() => {
    mockRequest.mockReset()
  })

  it('POST /api/v1/channels/lookup-models 并解包 models', async () => {
    mockRequest.mockResolvedValue({ models: ['m1', 'm2'] })
    const { models } = await channelApi.lookupModels({
      provider: 'openai_compatible',
      base_url: 'https://relay.example.com',
      api_key: 'sk-k',
    })
    expect(models).toEqual(['m1', 'm2'])
    expect(mockRequest).toHaveBeenCalledWith({
      url: '/api/v1/channels/lookup-models',
      method: 'POST',
      data: { provider: 'openai_compatible', base_url: 'https://relay.example.com', api_key: 'sk-k' },
    })
  })

  it('上游拉取失败时抛出 message', async () => {
    mockRequest.mockRejectedValue(new Error('HTTP 401: bad key'))
    await expect(
      channelApi.lookupModels({ provider: 'anthropic', base_url: 'https://a.com', api_key: 'k' }),
    ).rejects.toThrow('HTTP 401: bad key')
  })
})
