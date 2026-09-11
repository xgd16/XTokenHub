import { useState } from 'react'
import { compactCN } from '../utils/format'
import type { DonutSlice } from '../utils/transform'
import { useChartPalette } from '../theme'

interface Props {
  slices: DonutSlice[]
  total: number
  size?: number
  loading?: boolean
}

/** 环图（纯 SVG stroke-dasharray 实现）+ 右侧图例：悬浮扇区加粗、中心切换占比、图例同步高亮。 */
export default function ModelDonut({ slices, total, size = 220, loading = false }: Props) {
  const pal = useChartPalette()
  const [hover, setHover] = useState<string | null>(null)
  const R = 70
  const C = 2 * Math.PI * R

  // 加载中且无数据时显示骨架
  if (loading && slices.length === 0) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', gap: 20, flexWrap: 'wrap' }}>
        <div style={{ position: 'relative', width: size, height: size, flexShrink: 0, margin: '0 auto' }}>
          <svg viewBox="0 0 200 200" width={size} height={size}>
            <circle cx="100" cy="100" r={R} fill="none" stroke="var(--track-bg)" strokeWidth="26" className="skeleton-donut-ring" />
            <circle cx="100" cy="100" r={R} fill="none" stroke="var(--track-bg)" strokeWidth="26" strokeDasharray={`${C * 0.35} ${C * 0.65}`} strokeDashoffset={0} className="skeleton-donut-ring" style={{ animationDelay: '0.2s' }} opacity={0.5} />
          </svg>
        </div>
        <div style={{ flex: 1, minWidth: 240 }}>
          {[0, 1, 2, 3].map((i) => (
            <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '6px', borderBottom: '1px solid var(--border-faint)' }}>
              <span className="skeleton-bar" style={{ width: 9, height: 9, borderRadius: '50%', animationDelay: `${i * 0.1}s` }} />
              <span className="skeleton-bar" style={{ width: `${50 + i * 8}%`, height: 13, animationDelay: `${i * 0.1 + 0.05}s` }} />
              <span className="skeleton-bar" style={{ width: 36, height: 13, marginLeft: 'auto', animationDelay: `${i * 0.1 + 0.1}s` }} />
            </div>
          ))}
        </div>
      </div>
    )
  }
  let acc = 0
  const arcs = slices.map((s) => {
    const frac = total > 0 ? s.value / total : 0
    const arc = { ...s, dash: frac * C, offset: -acc * C }
    acc += frac
    return arc
  })
  const hovered = arcs.find((a) => a.name === hover)

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 20, flexWrap: 'wrap' }}>
      <div style={{ position: 'relative', width: size, height: size, flexShrink: 0, margin: '0 auto' }}>
        <svg
          viewBox="0 0 200 200"
          width={size}
          height={size}
          role="img"
          aria-label="模型用量占比"
          onMouseLeave={() => setHover(null)}
        >
          {/* 底环 */}
          <circle cx="100" cy="100" r={R} fill="none" stroke={pal.track} strokeWidth="26" />
          <g transform="rotate(-90 100 100)">
            {arcs.map((a) => {
              const active = hover === a.name
              return (
                <circle
                  key={a.name}
                  cx="100"
                  cy="100"
                  r={R}
                  fill="none"
                  stroke={a.color}
                  strokeWidth={active ? 32 : 26}
                  strokeDasharray={`${a.dash} ${C - a.dash}`}
                  strokeDashoffset={a.offset}
                  opacity={hover && !active ? 0.35 : 1}
                  style={{ cursor: 'pointer', transition: 'stroke-dasharray 0.5s ease, stroke-dashoffset 0.5s ease, stroke-width 0.15s ease, opacity 0.15s ease' }}
                  onMouseEnter={() => setHover(a.name)}
                />
              )
            })}
          </g>
        </svg>
        <div
          style={{
            position: 'absolute',
            inset: 0,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            pointerEvents: 'none',
            transition: 'opacity 0.15s ease',
          }}
        >
          {hovered ? (
            <>
              <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)', maxWidth: size * 0.7, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {hovered.name}
              </span>
              <span className="mono" style={{ fontSize: 22, fontWeight: 600, color: hovered.color }}>{hovered.percent}%</span>
              <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)' }}>{compactCN(hovered.value)} tokens</span>
            </>
          ) : (
            <>
              <span className="mono" style={{ fontSize: 22, fontWeight: 600 }}>{compactCN(total)}</span>
              <span style={{ fontSize: 12, color: 'var(--text-faint)' }}>tokens</span>
            </>
          )}
        </div>
      </div>

      <div style={{ flex: 1, minWidth: 240 }}>
        {arcs.map((s) => (
          <div
            key={s.name}
            onMouseEnter={() => setHover(s.name)}
            onMouseLeave={() => setHover(null)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '6px 6px',
              borderRadius: 6,
              background: hover === s.name ? pal.hoverBg : 'transparent',
              borderBottom: hover === s.name ? 'none' : '1px solid var(--border-faint)',
              cursor: 'default',
              transition: 'background 0.15s ease',
            }}
          >
            <span style={{ width: 9, height: 9, borderRadius: '50%', background: s.color, flexShrink: 0 }} />
            <span className="mono" style={{ fontSize: 13, color: 'var(--text-primary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {s.name}
            </span>
            <span className="mono" style={{ fontSize: 13, width: 52, textAlign: 'right' }}>{s.percent}%</span>
            <span className="mono" style={{ fontSize: 12, color: 'var(--text-faint)', width: 84, textAlign: 'right' }}>
              {compactCN(s.value)} tokens
            </span>
          </div>
        ))}
        {slices.length === 0 && (
          <div style={{ color: 'var(--text-faint)', padding: '24px 0', textAlign: 'center', fontSize: 12 }}>
            暂无数据 · 网关请求产生后实时出现
          </div>
        )}
      </div>
    </div>
  )
}
