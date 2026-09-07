import type { CSSProperties, ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useChartPalette } from '../theme'

interface Props {
  x?: number
  y: number
  children: ReactNode
  /** below=true 时显示在锚点下方（折线图避免遮挡曲线）。 */
  below?: boolean
  /** 可选容器宽度，用于水平钳制避免出界。 */
  wrapW?: number
  /** 传入时用百分比水平定位（SVG viewBox 缩放场景），0~100。 */
  leftPct?: number
  /** 以视口坐标渲染并 portal 到 body（position:fixed），不受任何祖先 overflow 裁剪。 */
  fixed?: boolean
}

/** 图表悬浮提示：默认绝对定位于 position:relative 父容器（x/y 为容器内坐标）；
 *  fixed=true 时以视口坐标 portal 到 body，不受祖先 overflow/stacking 裁剪。 */
export default function ChartTooltip({ x = 0, y, children, below, wrapW, leftPct, fixed }: Props) {
  const pal = useChartPalette()
  const base: CSSProperties = {
    background: pal.tooltipBg,
    border: `1px solid ${pal.tooltipBorder}`,
    borderRadius: 6,
    padding: '6px 10px',
    pointerEvents: 'none',
    whiteSpace: 'nowrap',
    fontSize: 12,
  }
  let style: CSSProperties
  if (fixed) {
    const cx = Math.max(90, Math.min(window.innerWidth - 90, x))
    style = { position: 'fixed', left: cx, top: y, zIndex: 2000, ...base }
  } else {
    const pos: CSSProperties = leftPct != null
      ? { left: `${Math.max(8, Math.min(92, leftPct))}%` }
      : { left: wrapW ? Math.max(70, Math.min(wrapW - 70, x)) : x }
    style = { position: 'absolute', ...pos, top: y, zIndex: 20, ...base }
  }
  const node = (
    <div
      style={{
        ...style,
        transform: below ? 'translate(-50%, 12px)' : 'translate(-50%, calc(-100% - 10px))',
        boxShadow: pal.tooltipShadow,
      }}
    >
      {children}
    </div>
  )
  return fixed ? createPortal(node, document.body) : node
}
