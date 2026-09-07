import { useId, useMemo, useRef, useState } from 'react'
import { compactCN } from '../utils/format'
import type { SeriesPoint } from '../utils/transform'
import { useChartPalette } from '../theme'
import ChartTooltip from './ChartTooltip'

const CHART_W = 800

/** y 轴上限取「好看」的整数（1/2/2.5/5/10 × 10^k），避免顶到图边。 */
function niceCeil(v: number): number {
  const pow = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) {
    if (m * pow >= v - 1e-9) return m * pow
  }
  return 10 * pow
}

interface Geometry {
  linePath: string
  areaPath: string
  dots: { x: number; y: number }[]
  xLabels: { x: number; text: string; anchor: 'start' | 'middle' | 'end' }[]
  yLabels: { y: number; text: string }[]
  peak?: { x: number; y: number; text: string }
  allZero: boolean
  empty: boolean
}

/** 数据 -> SVG 几何（纯函数，useMemo 包裹）。 */
function computeGeometry(data: SeriesPoint[], H: number): Geometry {
  const W = 800
  const PAD = { l: 46, r: 20, t: 22, b: 28 }
  const iw = W - PAD.l - PAD.r
  const ih = H - PAD.t - PAD.b
  if (data.length === 0) {
    return { linePath: '', areaPath: '', dots: [], xLabels: [], yLabels: [], allZero: true, empty: true }
  }
  const maxVal = Math.max(...data.map((d) => d.requests), 1)
  const yMax = niceCeil(maxVal)
  const x = (i: number) => (data.length <= 1 ? PAD.l + iw / 2 : PAD.l + (i / (data.length - 1)) * iw)
  const y = (v: number) => PAD.t + ih - (v / yMax) * ih

  const pts = data.map((d, i) => ({ x: x(i), y: y(d.requests) }))
  const linePath = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(1)} ${p.y.toFixed(1)}`).join(' ')
  const baseline = PAD.t + ih
  const areaPath = `${linePath} L${pts[pts.length - 1].x.toFixed(1)} ${baseline} L${pts[0].x.toFixed(1)} ${baseline} Z`

  const dots = pts.map((p) => ({ x: p.x, y: p.y }))

  // x 轴标签：最多 4 个均匀采样，首尾必含
  const n = data.length
  const idx = new Set<number>([0, n - 1])
  if (n > 2) {
    const step = Math.max(1, Math.ceil((n - 1) / 4))
    for (let i = step; i < n - 1; i += step) idx.add(i)
  }
  const xLabels = [...idx]
    .sort((a, b) => a - b)
    .map((i) => ({
      x: x(i),
      text: data[i].date.length >= 10 ? data[i].date.slice(5) : data[i].date,
      anchor: (i === 0 ? 'start' : i === n - 1 ? 'end' : 'middle') as 'start' | 'middle' | 'end',
    }))

  // y 轴：0 / 中点 / 上限 三档网格线
  const yLabels = [0, yMax / 2, yMax].map((v) => ({ y: y(v), text: compactCN(v) }))

  // 峰值标注
  let peak: Geometry['peak']
  const topIdx = data.reduce((best, d, i) => (d.requests > data[best].requests ? i : best), 0)
  if (data[topIdx].requests > 0) {
    peak = { x: x(topIdx), y: y(data[topIdx].requests), text: String(data[topIdx].requests) }
  }

  return {
    linePath,
    areaPath,
    dots,
    xLabels,
    yLabels,
    peak,
    allZero: data.every((d) => d.requests === 0),
    empty: false,
  }
}

/** 纯 SVG 请求趋势折线图：零第三方图表依赖，空数据/单点均安全渲染。 */
export default function TrendChart({ data, height = 230, ariaLabel = '请求趋势' }: { data: SeriesPoint[]; height?: number; ariaLabel?: string }) {
  const pal = useChartPalette()
  const gradId = useId()
  const svgRef = useRef<SVGSVGElement | null>(null)
  const [hoverIdx, setHoverIdx] = useState<number | null>(null)
  const geo = useMemo(() => computeGeometry(data, height), [data, height])

  /** 鼠标位置 -> 最近数据点下标（viewBox 坐标换算，与缩放无关）。 */
  const onMove = (e: React.MouseEvent<SVGSVGElement>) => {
    if (!svgRef.current || data.length === 0) return
    const rect = svgRef.current.getBoundingClientRect()
    const vx = ((e.clientX - rect.left) / rect.width) * CHART_W
    const n = data.length
    const iw = CHART_W - 46 - 20
    const i = Math.round(((vx - 46) / iw) * (n - 1))
    setHoverIdx(Math.max(0, Math.min(n - 1, i)))
  }

  if (geo.empty) {
    return (
      <div style={{ height, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-faint)', fontSize: 12 }}>
        暂无数据
      </div>
    )
  }

  return (
    <div style={{ position: 'relative' }}>
      <svg
        ref={svgRef}
        viewBox={`0 0 ${CHART_W} ${height}`}
        width="100%"
        style={{ display: 'block' }}
        role="img"
        aria-label={ariaLabel}
        onMouseMove={onMove}
        onMouseLeave={() => setHoverIdx(null)}
      >
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={pal.accent} stopOpacity="0.20" />
            <stop offset="100%" stopColor={pal.accent} stopOpacity="0.02" />
          </linearGradient>
        </defs>

        {/* 横向网格 + y 轴刻度 */}
        {geo.yLabels.map((l, i) => (
          <g key={`gy-${i}`}>
            <line x1="46" x2="780" y1={l.y} y2={l.y} stroke={pal.grid} strokeDasharray="3 4" />
            <text x="40" y={l.y + 3} textAnchor="end" fontSize="10" fill={pal.textFaint} style={{ fontFamily: 'var(--font-mono)' }}>
              {l.text}
            </text>
          </g>
        ))}

        {/* 面积 + 折线 */}
        <path d={geo.areaPath} fill={`url(#${gradId})`} />
        <path d={geo.linePath} fill="none" stroke={pal.accent} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />

        {/* 数据点（辉光） */}
        {geo.dots.map((p, i) => (
          <g key={`dot-${i}`}>
            <circle cx={p.x} cy={p.y} r="6.5" fill={pal.accent} opacity="0.16" />
            <circle cx={p.x} cy={p.y} r="2.8" fill={pal.accent} />
          </g>
        ))}

        {/* 峰值标注 */}
        {geo.peak && (
          <text
            x={Math.min(Math.max(geo.peak.x, 60), 766)}
            y={Math.max(geo.peak.y - 11, 12)}
            textAnchor="middle"
            fontSize="11"
            fill={pal.textSecondary}
            style={{ fontFamily: 'var(--font-mono)' }}
          >
            {geo.peak.text}
          </text>
        )}

        {/* 悬浮：十字准线 + 高亮点 */}
        {hoverIdx != null && geo.dots[hoverIdx] && (
          <g>
            <line x1={geo.dots[hoverIdx].x} x2={geo.dots[hoverIdx].x} y1="22" y2={height - 28} stroke={pal.cross} strokeDasharray="3 3" />
            <circle cx={geo.dots[hoverIdx].x} cy={geo.dots[hoverIdx].y} r="4.5" fill={pal.accent} stroke={pal.halo} strokeWidth="1.5" />
          </g>
        )}

        {/* x 轴日期 */}
        {geo.xLabels.map((l, i) => (
          <text key={`gx-${i}`} x={l.x} y={height - 8} textAnchor={l.anchor} fontSize="10" fill={pal.textFaint} style={{ fontFamily: 'var(--font-mono)' }}>
            {l.text}
          </text>
        ))}
      </svg>
      {hoverIdx != null && data[hoverIdx] && (
        <ChartTooltip leftPct={(geo.dots[hoverIdx]?.x ?? 0) / CHART_W * 100} y={14} below>
          <div className="mono" style={{ color: 'var(--text-faint)', marginBottom: 3 }}>{data[hoverIdx].date}</div>
          <div className="mono">请求数 {compactCN(data[hoverIdx].requests)}</div>
          {data[hoverIdx].errors > 0 && <div className="mono" style={{ color: pal.coral }}>错误 {compactCN(data[hoverIdx].errors)}</div>}
          {data[hoverIdx].tokens > 0 && <div className="mono" style={{ color: 'var(--text-faint)' }}>tokens {compactCN(data[hoverIdx].tokens)}</div>}
        </ChartTooltip>
      )}
      {geo.allZero && (
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
    </div>
  )
}
