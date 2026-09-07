import { useMemo, useState } from 'react'
import { compactCN } from '../utils/format'
import type { HeatmapData, HeatMode } from '../utils/transform'
import { useChartPalette } from '../theme'
import ChartTooltip from './ChartTooltip'

const CELL = 10
const GAP = 2
const PAD = { l: 6, r: 6, t: 20, b: 4 }

interface Props {
  data: HeatmapData
  /** weekly 模式悬浮文案带「当周」。 */
  mode?: HeatMode
  ariaLabel?: string
}

interface Hover {
  cx: number // 视口坐标：portal 到 body 的 fixed 浮层，不受 overflow 裁剪
  cy: number
  date: string
  text: string
}

/** GitHub 风格 Token 活动热力图：列为周（周日~周六），横向可滚动（移动端），悬浮显示明细。 */
export default function TokenHeatmap({ data, mode = 'daily', ariaLabel = 'Token 活动热力图' }: Props) {
  const pal = useChartPalette()
  const [hover, setHover] = useState<Hover | null>(null)

  const geo = useMemo(() => {
    const cols = data.columns.length
    const rows = Math.max(...data.columns.map((c) => c.cells.length), 1)
    const W = PAD.l + cols * (CELL + GAP) + PAD.r
    const H = PAD.t + rows * (CELL + GAP) + PAD.b
    return { cols, rows, W, H }
  }, [data])

  if (data.columns.length === 0) {
    return (
      <div style={{ height: 160, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-faint)', fontSize: 12 }}>
        暂无数据
      </div>
    )
  }

  const cellText = (date: string, value: number): string => {
    const m = Number(date.slice(5, 7))
    const d = Number(date.slice(8, 10))
    const suffix = mode === 'weekly' ? ' 当周' : ''
    return `${m}月${d}日${suffix} · ${compactCN(value)} tokens`
  }

  return (
    <div style={{ overflowX: 'auto' }}>
        <svg width={geo.W} height={geo.H} style={{ display: 'block', margin: '0 auto' }} role="img" aria-label={ariaLabel} onMouseLeave={() => setHover(null)}>
          {/* 月份标签 */}
          {data.monthLabels.map((m) => (
            <text
              key={`m-${m.index}`}
              x={PAD.l + m.index * (CELL + GAP)}
              y={13}
              fontSize="10"
              fill={pal.textFaint}
              style={{ fontFamily: 'var(--font-mono)' }}
            >
              {m.label}
            </text>
          ))}

          {/* 单元格 */}
          {data.columns.map((col, ci) =>
            col.cells.map((c, ri) => {
              const x = PAD.l + ci * (CELL + GAP)
              const y = PAD.t + ri * (CELL + GAP)
              return (
                <rect
                  key={c.date}
                  x={x}
                  y={y}
                  width={CELL}
                  height={CELL}
                  rx={2}
                  fill={pal.heat[c.level]}
                  stroke={hover?.date === c.date ? pal.hoverStroke : 'transparent'}
                  strokeWidth={1}
                  onMouseEnter={(e) => setHover({ cx: e.clientX, cy: e.clientY, date: c.date, text: cellText(c.date, c.value) })}
                />
              )
            }),
          )}
        </svg>
      {hover && (
        <ChartTooltip fixed x={hover.cx} y={hover.cy - 10}>
          {hover.text}
        </ChartTooltip>
      )}
    </div>
  )
}
