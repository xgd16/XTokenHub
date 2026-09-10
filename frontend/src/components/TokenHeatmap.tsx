import { useEffect, useMemo, useRef, useState } from 'react'
import { compactCN } from '../utils/format'
import type { HeatmapData, HeatMode } from '../utils/transform'
import { useChartPalette } from '../theme'
import ChartTooltip from './ChartTooltip'

/** 回退格子尺寸：容器尚未测量到（首帧/测试环境）时使用。 */
const FALLBACK_CELL = 10
/** 自适应下限：低于该尺寸改为横向滚动，保证可点。 */
const MIN_CELL = 8
/** 自适应上限：避免超宽屏下格子大得突兀。 */
const MAX_CELL = 26
/** 间隙随格子等比缩放。 */
const GAP_RATIO = 0.18
const PAD = { l: 6, r: 6 }

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

/** GitHub 风格 Token 活动热力图：格子尺寸随容器宽高自适应（移动端过窄时横向可滚动），悬浮显示明细。 */
export default function TokenHeatmap({ data, mode = 'daily', ariaLabel = 'Token 活动热力图' }: Props) {
  const pal = useChartPalette()
  const [hover, setHover] = useState<Hover | null>(null)
  const boxRef = useRef<HTMLDivElement | null>(null)
  const [box, setBox] = useState<{ w: number; h: number } | null>(null)

  useEffect(() => {
    const el = boxRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      const r = entries[0].contentRect
      setBox({ w: r.width, h: r.height })
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const geo = useMemo(() => {
    const cols = data.columns.length
    const rows = Math.max(...data.columns.map((c) => c.cells.length), 1)

    let cell = FALLBACK_CELL
    if (box && box.w > 0) {
      // 间隙按 GAP_RATIO 随格子缩放，反解：cell = 可用宽 / (cols + (cols-1)*ratio)
      const byW = (box.w - PAD.l - PAD.r) / (cols + (cols - 1) * GAP_RATIO)
      const byH = box.h > 0 ? (box.h - 30) / (rows + (rows - 1) * GAP_RATIO) : Infinity
      const fitted = Math.floor(Math.min(byW, byH))
      if (fitted >= MIN_CELL) cell = Math.min(MAX_CELL, fitted)
      else cell = MIN_CELL // 容器过窄：保底尺寸 + 横向滚动
    }
    const gap = Math.max(2, Math.round(cell * GAP_RATIO))
    const labelFs = Math.max(10, Math.min(13, Math.round(cell * 0.7)))
    const padT = labelFs + 8
    return {
      cols,
      rows,
      cell,
      gap,
      labelFs,
      padT,
      W: PAD.l + cols * (cell + gap) - gap + PAD.r,
      H: padT + rows * (cell + gap) - gap + 4,
    }
  }, [data, box])

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
    <div
      ref={boxRef}
      style={{ flex: 1, minHeight: 120, overflowX: 'auto', display: 'flex', alignItems: 'center' }}
    >
      <svg
        width={geo.W}
        height={geo.H}
        style={{ display: 'block', margin: '0 auto', maxWidth: 'none' }}
        role="img"
        aria-label={ariaLabel}
        onMouseLeave={() => setHover(null)}
      >
        {/* 月份标签 */}
        {data.monthLabels.map((m) => (
          <text
            key={`m-${m.index}`}
            x={PAD.l + m.index * (geo.cell + geo.gap)}
            y={geo.labelFs + 2}
            fontSize={geo.labelFs}
            fill={pal.textFaint}
            style={{ fontFamily: 'var(--font-mono)' }}
          >
            {m.label}
          </text>
        ))}

        {/* 单元格 */}
        {data.columns.map((col, ci) =>
          col.cells.map((c, ri) => {
            const x = PAD.l + ci * (geo.cell + geo.gap)
            const y = geo.padT + ri * (geo.cell + geo.gap)
            return (
              <rect
                key={c.date}
                x={x}
                y={y}
                width={geo.cell}
                height={geo.cell}
                rx={Math.max(2, Math.round(geo.cell * 0.16))}
                fill={pal.heat[c.level]}
                stroke={hover?.date === c.date ? pal.hoverStroke : 'transparent'}
                strokeWidth={geo.cell > 14 ? 2 : 1}
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
