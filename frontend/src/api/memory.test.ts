import { beforeEach, describe, expect, it, vi } from 'vitest'
import { memoryApi, tagsOf } from './memory'
import { request } from './http'

vi.mock('./http', () => ({ request: vi.fn() }))

const mockRequest = vi.mocked(request)

describe('tagsOf', () => {
  it('拆分逗号分隔标签并去空白', () => {
    expect(tagsOf({ tags: ' 支付 , 幂等,, ' })).toEqual(['支付', '幂等'])
  })

  it('空串返回空数组', () => {
    expect(tagsOf({ tags: '' })).toEqual([])
  })
})

describe('memoryApi', () => {
  beforeEach(() => {
    mockRequest.mockReset()
  })

  it('search 命中检索端点并透传过滤参数', async () => {
    mockRequest.mockResolvedValue({ items: [], count: 0, namespaces: [], mode: 'fts' })
    await memoryApi.search({ q: '幂等', namespace: '支付回调', limit: 5 })
    expect(mockRequest).toHaveBeenCalledWith({
      url: '/api/v1/memories/search', method: 'GET',
      params: { q: '幂等', namespace: '支付回调', limit: 5 },
    })
  })

  it('create 写入并返回去重标记', async () => {
    mockRequest.mockResolvedValue({ memory: { id: 1 }, duplicated: true, superseded: [], links: [] })
    const res = await memoryApi.create({ content: '内容', namespace: 'ns', supersedes: [3] })
    expect(res.duplicated).toBe(true)
    expect(mockRequest).toHaveBeenCalledWith({
      url: '/api/v1/memories', method: 'POST',
      data: { content: '内容', namespace: 'ns', supersedes: [3] },
    })
  })

  it('supersede 提交 successor_id', async () => {
    mockRequest.mockResolvedValue({ id: 1, superseded_by: 2 })
    await memoryApi.supersede(1, 2)
    expect(mockRequest).toHaveBeenCalledWith({
      url: '/api/v1/memories/1/supersede', method: 'POST', data: { successor_id: 2 },
    })
  })

  it('triggerClassify 触发分类任务', async () => {
    mockRequest.mockResolvedValue({ status: 'running', total: 3, done: 0, assigned: 0, new_namespaces: 0, message: '' })
    const st = await memoryApi.triggerClassify()
    expect(st.status).toBe('running')
    expect(mockRequest).toHaveBeenCalledWith({ url: '/api/v1/memories/classify', method: 'POST' })
  })
})
