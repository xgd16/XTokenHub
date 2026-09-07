import type { ReactNode } from 'react'
import type { RequestLog } from '../api/log'
import { compactCN, duration, modeShort, percent, protocolShort, tokenSpeed } from '../utils/format'

/** 通用键值详情行（移动端表格展开行用）。 */
export function DetailRows({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 5, fontSize: 12, padding: '4px 2px' }}>
      {rows.map(([k, v], i) => (
        <div key={`${k}-${i}`} style={{ display: 'flex', gap: 12 }}>
          <span style={{ color: 'var(--text-faint)', flexShrink: 0, minWidth: 44 }}>{k}</span>
          <span className="mono" style={{ color: 'var(--text-secondary)', wordBreak: 'break-all' }}>{v}</span>
        </div>
      ))}
    </div>
  )
}

/** 请求详情键值对（移动端表格展开行用；pending 为 true 时显示进行中状态）。 */
export default function LogDetail({ r, pending }: { r: RequestLog; pending?: boolean }) {
  const rows: [string, string][] = [
    ['协议', protocolShort(r.protocol)],
    ['模式', r.forward_mode ? modeShort(r.forward_mode) : '—'],
    ['渠道', r.channel_name || '—'],
    ['调用方', r.key_name || '（匿名）'],
    ['客户端', r.user_agent || '—'],
    ['流式', r.stream ? 'SSE' : '否'],
    ['Tokens', `总 ${compactCN(r.total_tokens)} · 入 ${compactCN(r.prompt_tokens)} · 出 ${compactCN(r.completion_tokens)}`],
    ['缓存', `${compactCN(r.cached_tokens)}（${percent(r.cache_hit_rate ?? 0)}）`],
    ['耗时', pending ? '进行中…' : duration(r.duration_ms)],
    ['速度', pending ? '—' : tokenSpeed(r.completion_tokens, r.duration_ms)],
    ['IP', r.client_ip || '—'],
  ]
  if (r.error) rows.push(['错误', r.error])
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 5, fontSize: 12, padding: '4px 2px' }}>
      {rows.map(([k, v]) => (
        <div key={k} style={{ display: 'flex', gap: 12 }}>
          <span style={{ color: 'var(--text-faint)', flexShrink: 0, minWidth: 44 }}>{k}</span>
          <span className="mono" style={{ color: k === '错误' ? 'var(--coral)' : 'var(--text-secondary)', wordBreak: 'break-all' }}>{v}</span>
        </div>
      ))}
    </div>
  )
}
