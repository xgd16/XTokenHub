import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Table } from 'antd'
import type { RequestLog } from '../api/log'
import { MOBILE_LOG_COLUMNS } from './RequestLogs'

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
  duration_ms: 10,
  upstream_status: 200,
  client_ip: '',
  user_agent: '',
  error: '',
} as RequestLog

describe('移动端日志表列定义', () => {
  it('状态列声明了 dataIndex，避免整行对象被当状态值渲染', () => {
    const status = MOBILE_LOG_COLUMNS.find((c) => c.key === 'status')
    expect(status && 'dataIndex' in status ? status.dataIndex : undefined).toBe('upstream_status')
  })

  it('成功行渲染出上游状态码而非崩溃', () => {
    render(<Table rowKey="id" dataSource={[row]} pagination={false} columns={MOBILE_LOG_COLUMNS} />)
    expect(screen.getByText('200')).toBeTruthy()
  })
})
