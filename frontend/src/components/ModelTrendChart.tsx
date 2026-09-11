import { useMemo, useRef, useState } from 'react'
import { compactCN } from '../utils/format'
import { MODEL_COLORS, type ModelTrendData } from '../utils/transform'
import { useChartPalette } from '../theme'
import ChartTooltip from './ChartTooltip'

/** y 轴上限取「好看」的整数（1/2/2.5/5/10 × 10^k）。 */
function niceCeil(v: number): number {
  if (v <= 0) return 1
  const pow = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) {
    if (m * pow >= v - 1e-9) return m * pow
  }
  return 10 * pow
}

/** Catmull-Rom -> 三次贝塞尔平滑路径（与截图中的圆滑曲线一致）。 */
function smoothPath(pts: { x: number; y: number }[]): string {
  if (pts.length === 0) return ''
  if (pts.length === 1) return `M${pts[0].x.toFixed(1)} ${pts[0].y.toFixed(1)}`
  let d = `M${pts[0].x.toFixed(1)} ${pts[0].y.toFixed(1)}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[Math.max(0, i - 1)]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[Math.min(pts.length - 1, i + 2)]
    const c1x = p1.x + (p2.x - p0.x) / 6
    const c1y = p1.y + (p2.y - p0.y) / 6
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = p2.y - (p3.y - p1.y) / 6
    d += ` C${c1x.toFixed(1)} ${c1y.toFixed(1)}, ${c2x.toFixed(1)} ${c2y.toFixed(1)}, ${p2.x.toFixed(1)} ${p2.y.toFixed(1)}`
  }
  return d
}

interface Props {
  data: ModelTrendData
  height?: number
  ariaLabel?: string
  loading?: boolean
}

const W = 800
const PAD = { l: 52, r: 20, t: 18, b: 28 }

/** 纯 SVG 多模型每日 token 趋势图：平滑曲线 + 图例 + 十字准线悬浮明细，零第三方图表依赖。 */
export default function ModelTrendChart({ data, height = 260, ariaLabel = '每日 Token 趋势图', loading = false }: Props) {
  const pal = useChartPalette()
  const svgRef = useRef<SVGSVGElement | null>(null)
  const [hoverIdx, setHoverIdx] = useState<number | null>(null)

  const geo = useMemo(() => {
    const iw = W - PAD.l - PAD.r
    const ih = height - PAD.t - PAD.b
    const n = data.dates.length
    const maxVal = Math.max(...data.series.flatMap((s) => s.values), 1)
    const yMax = niceCeil(maxVal)
    const x = (i: number) => (n <= 1 ? PAD.l + iw / 2 : PAD.l + (i / (n - 1)) * iw)
    const y = (v: number) => PAD.t + ih - (v / yMax) * ih
    const paths = data.series.map((s, si) => ({
      name: s.name,
      color: MODEL_COLORS[si % MODEL_COLORS.length],
      d: smoothPath(s.values.map((v, i) => ({ x: x(i), y: y(v) }))),
      values: s.values,
    }))
    const idx = new Set<number>([0, n - 1])
    if (n > 2) {
      const step = Math.max(1, Math.ceil((n - 1) / 4))
      for (let i = step; i < n - 1; i += step) idx.add(i)
    }
    const xLabels = [...idx].sort((a, b) => a - b).map((i) => ({
      x: x(i),
      text: data.dates[i].length >= 10 ? `${Number(data.dates[i].slice(5, 7))}月${Number(data.dates[i].slice(8, 10))}日` : data.dates[i],
      anchor: (i === 0 ? 'start' : i === n - 1 ? 'end' : 'middle') as 'start' | 'middle' | 'end',
    }))
    const yLabels = [0, yMax / 2, yMax].map((v) => ({ y: y(v), text: compactCN(v) }))
    return { iw, ih, n, x, y, xLabels, yLabels, paths, empty: data.series.length === 0 || maxVal <= 1 }
  }, [data, height])

  /** 鼠标位置 -> 最近数据点下标（viewBox 坐标换算，与缩放无关）。 */
  const onMove = (e: React.MouseEvent<SVGSVGElement>) => {
    if (!svgRef.current || geo.n === 0) return
    const rect = svgRef.current.getBoundingClientRect()
    const vx = ((e.clientX - rect.left) / rect.width) * W
    const i = Math.round(((vx - PAD.l) / geo.iw) * (geo.n - 1))
    const next = Math.max(0, Math.min(geo.n - 1, i))
    // mousemove 可达上百次/秒：同一桶内移动时保持原引用，避免整张 SVG 无谓重渲染
    setHoverIdx((prev) => (prev === next ? prev : next))
  }

  const hoverRows =
    hoverIdx == null
      ? []
      : geo.paths
          .map((p) => ({ name: p.name, color: p.color, value: p.values[hoverIdx] }))
          .filter((r) => r.value > 0)
          .sort((a, b) => b.value - a.value)

  // 加载中且无数据时显示骨架
  if (loading && geo.empty) {
    const iw = W - PAD.l - PAD.r
    const ih = height - PAD.t - PAD.b
    const waveColors = ['var(--track-bg)', 'rgba(94,128,148,0.08)']
    return (
      <div>
        <div style={{ display: 'flex', gap: 12, marginBottom: 8 }}>
          {[56, 72, 48].map((w, i) => (
            <span key={i} className="skeleton-bar" style={{ width: w, height: 12, animationDelay: `${i * 0.1}s` }} />
          ))}
        </div>
        <svg viewBox={`0 0 ${W} ${height}`} width="100%" style={{ display: 'block' }}>
          {[0, 0.5, 1].map((f) => (
            <line key={f} x1={PAD.l} x2={W - PAD.r} y1={PAD.t + ih * f} y2={PAD.t + ih * f} stroke="var(--track-bg)" strokeDasharray="3 4" />
          ))}
          {[0.3, 0.55, 0.75].map((base, si) => {
            const pts = Array.from({ length: 10 }, (_, i) => {
              const x = PAD.l + (i / 9) * iw
              const y = PAD.t + ih * (base + 0.12 * Math.sin(i * 0.9 + si * 2))
              return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`
            }).join(' ')
            return <path key={si} d={pts} fill="none" stroke={waveColors[si % 2]} strokeWidth="2" strokeLinecap="round" className="skeleton-chart-wave" style={{ animationDelay: `${si * 0.2}s` }} />
          })}
        </svg>
      </div>
    )
  }

  return (
    <div>
      {/* 图例 */}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 16px', marginBottom: 8 }}>
        {geo.paths.map((p) => (
          <span key={p.name} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12 }}>
            <span style={{ width: 8, height: 8, borderRadius: '50%', background: p.color, flexShrink: 0 }} />
            <span className="mono" style={{ color: 'var(--text-secondary)' }}>{p.name}</span>
          </span>
        ))}
      </div>

      <div style={{ position: 'relative' }}>
        <svg
          ref={svgRef}
          viewBox={`0 0 ${W} ${height}`}
          width="100%"
          style={{ display: 'block' }}
          role="img"
          aria-label={ariaLabel}
          onMouseMove={onMove}
          onMouseLeave={() => setHoverIdx(null)}
        >
          {geo.yLabels.map((l, i) => (
            <g key={`gy-${i}`}>
              <line x1={PAD.l} x2={W - PAD.r} y1={l.y} y2={l.y} stroke={pal.grid} strokeDasharray="3 4" />
              <text x={PAD.l - 8} y={l.y + 3} textAnchor="end" fontSize="10" fill={pal.textFaint} style={{ fontFamily: 'var(--font-mono)' }}>
                {l.text}
              </text>
            </g>
          ))}

          {geo.paths.map((p) => (
            <path key={p.name} d={p.d} fill="none" stroke={p.color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          ))}

          {/* 十字准线 + 各系列高亮点 */}
          {hoverIdx != null && (
            <g>
              <line x1={geo.x(hoverIdx)} x2={geo.x(hoverIdx)} y1={PAD.t} y2={PAD.t + geo.ih} stroke={pal.cross} strokeDasharray="3 3" />
              {geo.paths.map((p) =>
                p.values[hoverIdx] > 0 ? (
                  <circle key={`hv-${p.name}`} cx={geo.x(hoverIdx)} cy={geo.y(p.values[hoverIdx])} r="3.5" fill={p.color} stroke={pal.halo} strokeWidth="1.5" />
                ) : null,
              )}
            </g>
          )}

          {geo.xLabels.map((l, i) => (
            <text key={`gx-${i}`} x={l.x} y={height - 8} textAnchor={l.anchor} fontSize="10" fill={pal.textFaint} style={{ fontFamily: 'var(--font-mono)' }}>
              {l.text}
            </text>
          ))}
        </svg>
        {geo.empty && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--text-faint)',
              fontSize: 12,
              pointerEvents: 'none',
            }}
          >
            暂无数据 · 网关请求产生后实时出现
          </div>
        )}
        {hoverIdx != null && hoverRows.length > 0 && (
          <ChartTooltip leftPct={(geo.x(hoverIdx) / W) * 100} y={16} below>
            <div className="mono" style={{ color: 'var(--text-faint)', marginBottom: 3 }}>
              {Number(data.dates[hoverIdx].slice(5, 7))}月{Number(data.dates[hoverIdx].slice(8, 10))}日
            </div>
            {hoverRows.map((r) => (
              <div key={r.name} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                <span style={{ width: 7, height: 7, borderRadius: '50%', background: r.color, flexShrink: 0 }} />
                <span className="mono" style={{ maxWidth: 150, overflow: 'hidden', textOverflow: 'ellipsis' }}>{r.name}</span>
                <span className="mono" style={{ marginLeft: 'auto', paddingLeft: 10 }}>{compactCN(r.value)}</span>
              </div>
            ))}
          </ChartTooltip>
        )}
      </div>
    </div>
  )
}
