import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Table } from 'antd'
import type { RequestLog } from '../api/log'
import { formatMoney } from '../utils/money'
import { costTip, mobileLogColumns } from './RequestLogs'

/**
 * 回归：状态列必须显式声明 dataIndex。
 * 此前移动端日志表漏了 dataIndex，rc-table 会把整行 record 传给 render 的第一个参数，
 * 非 pending 分支直接渲染 {v} 会抛 "Objects are not valid as a React child"。
 */
const row = {
  id: 1,
  created_at: '2026-01-01T00:00:00Z',
  protocol: 'chat_completions',
  forward_mode: 'converted',
  channel_id: 1,
  channel_name: 'c',
  key_id: 1,
  key_name: 'k',
  model: 'm',
  stream: false,
  prompt_tokens: 1,
  completion_tokens: 1,
  total_tokens: 2,
  cached_tokens: 0,
  cache_write_tokens: 0,
  cache_hit_rate: 0,
  cost_usd: 0.0123,
  duration_ms: 10,
  upstream_status: 200,
  client_ip: '',
  user_agent: '',
  error: '',
} as RequestLog

/** 展示币种固定 USD，便于断言金额文本。 */
const money = (usd: number) => formatMoney(usd, { currency: 'USD', rate: 0 })

describe('移动端日志表列定义', () => {
  it('状态列声明了 dataIndex，避免整行对象被当状态值渲染', () => {
    const status = mobileLogColumns(money).find((c) => c.key === 'status')
    expect(status && 'dataIndex' in status ? status.dataIndex : undefined).toBe('upstream_status')
  })

  it('成功行渲染出上游状态码而非崩溃', () => {
    render(<Table rowKey="id" dataSource={[row]} pagination={false} columns={mobileLogColumns(money)} />)
    expect(screen.getByText('200')).toBeTruthy()
  })

  it('花费列渲染出请求费用', () => {
    expect(mobileLogColumns(money).some((c) => c.key === 'cost')).toBe(true)
    render(<Table rowKey="id" dataSource={[row]} pagination={false} columns={mobileLogColumns(money)} />)
    expect(screen.getByText('$0.0123')).toBeTruthy()
  })

  it('花费悬浮说明同时体现计价口径与命中的时段', () => {
    // 仅有口径：只显示口径（未配时段价的模型）
    expect(costTip({ ...row, usage_style: 'openai' } as RequestLog)).toBe('计价口径 openai')
    // 口径 + 空闲时段：错峰价应显式说明，便于解释「为何更便宜」
    expect(costTip({ ...row, usage_style: 'openai', price_period: 'off_peak' } as RequestLog)).toBe(
      '计价口径 openai · 空闲时段（错峰价）',
    )
    expect(costTip({ ...row, usage_style: 'anthropic', price_period: 'peak' } as RequestLog)).toBe(
      '计价口径 anthropic · 高峰时段',
    )
    // 无口径无时段（未定价模型）
    expect(costTip(row)).toBe('未定价模型记 0')
  })
})
