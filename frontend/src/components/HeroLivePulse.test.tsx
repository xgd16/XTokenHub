import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import HeroLivePulse, { mergePulseRows } from './HeroLivePulse'
import type { RequestLog } from '../api/log'

/** 造一条已落库请求（默认成功）。 */
function done(patch: Partial<RequestLog> = {}): RequestLog {
  return {
    id: 1,
    created_at: '2026-10-14T07:00:00Z',
    protocol: 'openai',
    forward_mode: 'native_passthrough',
    channel_id: 1,
    channel_name: '主通道',
    key_id: 1,
    key_name: 'k',
    model: 'deepseek-v4.1-flash',
    stream: true,
    prompt_tokens: 100,
    completion_tokens: 50,
    total_tokens: 150,
    cached_tokens: 0,
    cache_write_tokens: 0,
    cache_hit_rate: 0,
    cost_usd: 0,
    duration_ms: 2000,
    upstream_status: 200,
    client_ip: '127.0.0.1',
    user_agent: 'curl/8',
    error: '',
    ...patch,
  }
}

/** 造一条进行中的请求：无落库 id，只有进程内 req_id。 */
function running(patch: Partial<RequestLog> = {}): RequestLog {
  return done({ id: 0, req_id: 77, total_tokens: 0, completion_tokens: 0, duration_ms: 0, ...patch })
}

describe('mergePulseRows', () => {
  it('按时间倒序并截断到 limit 条', () => {
    const seed = [
      done({ id: 1, created_at: '2026-10-14T07:00:00Z' }),
      done({ id: 3, created_at: '2026-10-14T07:02:00Z' }),
      done({ id: 2, created_at: '2026-10-14T07:01:00Z' }),
    ]
    expect(mergePulseRows(seed, [], 2).map((r) => r.id)).toEqual([3, 2])
  })

  it('实时缓冲按 id 覆盖同一条种子行（完成事件比种子更新）', () => {
    const seed = [done({ id: 5, total_tokens: 0, upstream_status: 0 })]
    const live = [done({ id: 5, total_tokens: 900, upstream_status: 200 })]
    const rows = mergePulseRows(seed, live)
    expect(rows).toHaveLength(1)
    expect(rows[0].total_tokens).toBe(900)
  })

  it('进行中的行以 req_id 为键，不与种子里的历史行混淆', () => {
    // 进行中的请求必然是最新一条：种子是刚落库的历史行
    const rows = mergePulseRows([done({ id: 9 })], [running({ created_at: '2026-10-14T07:05:00Z' })])
    expect(rows.map((r) => r.id)).toEqual([0, 9])
  })
})

describe('HeroLivePulse', () => {
  it('冷启动：live 为空时用种子渲染，不留空白', () => {
    // 回归点：live 只收 WS 事件，页面刚打开必然为空；此前 hero 右侧因此长期是空白区
    const { container } = render(<HeroLivePulse seed={[done({ id: 4 })]} live={[]} />)
    const rows = container.querySelectorAll('.obs-pulse-row')
    expect(rows).toHaveLength(1)
    expect(container.textContent).toContain('deepseek-v4.1-flash')
  })

  it('无任何数据时显示等待占位而不是空白', () => {
    const { container } = render(<HeroLivePulse />)
    expect(container.querySelectorAll('.obs-pulse-empty')).toHaveLength(1)
    expect(container.textContent).toContain('等待请求')
    expect(container.textContent).toContain('暂无请求')
  })

  it('loading 期间显示加载中，且不误报为空', () => {
    const { container } = render(<HeroLivePulse loading />)
    expect(container.textContent).toContain('加载中')
    // 加载中不展示「网关空闲」——那会把「还没拉到」说成「确实没有流量」
    expect(container.textContent).not.toContain('网关当前空闲')
  })

  it('成功 / 失败 / 进行中分别打上对应状态点', () => {
    const { container } = render(
      <HeroLivePulse
        live={[
          running(),
          done({ id: 2, error: 'boom', upstream_status: 500 }),
          done({ id: 3 }),
        ]}
      />,
    )
    expect(container.querySelector('.obs-pulse-row.running .obs-pulse-dot.running')).not.toBeNull()
    expect(container.querySelector('.obs-pulse-row.error .obs-pulse-dot.error')).not.toBeNull()
    expect(container.querySelector('.obs-pulse-row.ok .obs-pulse-dot.ok')).not.toBeNull()
  })

  it('无在途请求时头部呼吸灯置灰', () => {
    const { container } = render(<HeroLivePulse live={[done({ id: 1 })]} />)
    expect(container.querySelector('.obs-pulse-head .live-dot.off')).not.toBeNull()
  })

  it('有在途请求时呼吸灯点亮', () => {
    const { container } = render(<HeroLivePulse live={[running()]} />)
    const dot = container.querySelector('.obs-pulse-head .live-dot')!
    expect(dot.classList.contains('off')).toBe(false)
  })

  it('头部汇总反映进行中与失败计数', () => {
    const { container } = render(
      <HeroLivePulse live={[running(), done({ id: 2, error: 'x', upstream_status: 400 }), done({ id: 3 })]} />,
    )
    const meta = container.querySelector('.obs-pulse-meta')!.textContent!
    expect(meta).toContain('进行中 1')
    expect(meta).toContain('失败 1')
  })

  it('全部成功时给出成功汇总', () => {
    const { container } = render(<HeroLivePulse live={[done({ id: 1 }), done({ id: 2 })]} />)
    expect(container.querySelector('.obs-pulse-meta')!.textContent).toContain('全部成功')
  })

  it('进行中的行不显示 tokens 与速度（数值尚未产生）', () => {
    const { container } = render(<HeroLivePulse live={[running()]} />)
    const row = container.querySelector('.obs-pulse-row.running')!
    expect(row.querySelector('.obs-pulse-tok')!.textContent).toBe('—')
    expect(row.querySelector('.obs-pulse-spd')!.textContent).toBe('—')
  })

  it('缺失 created_at / model 时回退为占位符而非崩溃', () => {
    const { container } = render(<HeroLivePulse live={[done({ id: 1, created_at: '', model: '' })]} />)
    const row = container.querySelector('.obs-pulse-row')!
    expect(row.querySelector('.obs-pulse-time')!.textContent).toBe('—')
    expect(row.querySelector('.obs-pulse-model')!.textContent).toBe('—')
  })
})