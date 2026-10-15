import { memo, useMemo } from 'react'
import LiveDuration from './LiveDuration'
import { type RequestLog } from '../api/log'
import { compactCN, duration, timeOf, tokenSpeed } from '../utils/format'

/** 固定展示条数：行高 + 列表 min-height 恒定，hero 高度不随数据量跳动。 */
const PULSE_ROWS = 6

/** 行键：已落库行用 id，进行中行（无 id）用进程内 req_id；两者皆无时退回时间戳组合。 */
function rowKey(r: RequestLog): string {
  if (r.id) return `id:${r.id}`
  if (r.req_id) return `req:${r.req_id}`
  return `ts:${r.created_at}:${r.channel_id ?? 0}`
}

/** 行状态：running=进行中 / error=失败 / ok=成功。 */
type PulseState = 'running' | 'error' | 'ok'

/**
 * 状态判定：进行中行无落库 id；否则有 error 或上游 4xx/5xx 即失败。
 *
 * `upstream_status === 0` 也算失败：表示请求没拿到任何上游响应（连接被拒等）。
 * 实测线上 200 条日志里这种组合出现 0 次，属兜底而非主路径——失败指示灯应当 fail-closed，
 * 不能给一条根本没拿到响应的请求打绿点。
 */
function stateOf(r: RequestLog): PulseState {
  if (!r.id && r.req_id) return 'running'
  if (r.error) return 'error'
  const st = r.upstream_status ?? 0
  return st >= 400 || st === 0 ? 'error' : 'ok'
}

/**
 * 合并「最近落库日志」与「本次会话的实时缓冲」：实时行更新更晚，按行键覆盖种子行，
 * 再按时间倒序取最近 limit 条。
 *
 * 为什么需要种子：live 只收 WebSocket 推来的事件，**页面刚打开时必然为空**——
 * 而当天往往已经有大量请求。只用 live 会让 hero 右侧在冷启动时是一整块空白，
 * 补上「最近几条已落库请求」后，首屏数据到达前也有内容可看。
 */
export function mergePulseRows(seed: RequestLog[], live: RequestLog[], limit = PULSE_ROWS): RequestLog[] {
  const map = new Map<string, RequestLog>()
  for (const r of seed) map.set(rowKey(r), r)
  for (const r of live) map.set(rowKey(r), r)
  return [...map.values()]
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, limit)
}

/** 汇总条右侧的一句话状态（进行中优先，其次失败，全成功则报成功）。 */
function metaText(rows: RequestLog[], loading: boolean): string {
  if (rows.length === 0) return loading ? '加载中' : '暂无请求'
  const running = rows.filter((r) => stateOf(r) === 'running').length
  const failed = rows.filter((r) => stateOf(r) === 'error').length
  if (running > 0) return `进行中 ${running}${failed > 0 ? ` · 失败 ${failed}` : ''}`
  return failed > 0 ? `失败 ${failed} / ${rows.length}` : `全部成功 · ${rows.length}`
}

interface HeroLivePulseProps {
  /** 最近落库的请求（页面加载时拉一次，作为实时缓冲的冷启动种子）。 */
  seed?: RequestLog[]
  /** 实时缓冲：新→旧。 */
  live?: RequestLog[]
  /** 首屏骨架（seed 尚未返回）。 */
  loading?: boolean
}

/**
 * Hero 右侧「实时活动」：最近几条请求的一行式速览
 * （时间 / 模型 / 状态点 / Tokens / 耗时 / 输出速度）。
 *
 * 与页面底部的「实时请求流」是两种视角——那是按会话分组的完整表格，这里是页首的
 * 一眼概览：网关此刻在处理什么、有没有出错。
 */
const HeroLivePulse = memo(function HeroLivePulse({ seed = [], live = [], loading = false }: HeroLivePulseProps) {
  const rows = useMemo(() => mergePulseRows(seed, live), [seed, live])
  const idle = rows.length === 0
  // 头部呼吸灯只在确有在途请求时闪：无在途置灰，避免「实时」信号一直亮着却其实没流量
  const busy = useMemo(() => rows.some((r) => stateOf(r) === 'running'), [rows])

  return (
    <aside className="obs-pulse" aria-label="实时活动">
      <div className="obs-pulse-head">
        <span className={`live-dot${busy ? '' : ' off'}`} aria-hidden="true" />
        <span className="obs-panel-title">实时活动</span>
        <span className="obs-pulse-meta mono">{metaText(rows, loading)}</span>
      </div>
      <ul className="obs-pulse-list">
        {idle ? (
          // 空态占满列表的预留高度：既保持 hero 高度恒定，又不留一块无解释的空白
          <li className="obs-pulse-empty">
            <span className="obs-pulse-empty-title">{loading ? '加载中…' : '等待请求 · 网关当前空闲'}</span>
            {!loading && <span className="obs-pulse-empty-sub">新请求经 WebSocket 实时推送到这里</span>}
          </li>
        ) : (
          rows.map((r) => {
            const st = stateOf(r)
            return (
              <li className={`obs-pulse-row ${st}`} key={rowKey(r)}>
                <span className={`obs-pulse-dot ${st}`} aria-hidden="true" />
                <span className="obs-pulse-time mono">{r.created_at ? timeOf(r.created_at) : '—'}</span>
                <span className="obs-pulse-model mono" title={r.model || undefined}>
                  {r.model || '—'}
                </span>
                <span className="obs-pulse-tok mono">{st === 'running' ? '—' : compactCN(r.total_tokens)}</span>
                <span className="obs-pulse-dur mono">
                  {st === 'running' ? <LiveDuration from={r.created_at} /> : duration(r.duration_ms)}
                </span>
                <span className="obs-pulse-spd mono">{st === 'running' ? '—' : tokenSpeed(r.completion_tokens, r.duration_ms)}</span>
              </li>
            )
          })
        )}
      </ul>
    </aside>
  )
})

export default HeroLivePulse