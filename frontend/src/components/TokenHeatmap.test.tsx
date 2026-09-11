import { act, fireEvent, render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TokenHeatmap from './TokenHeatmap'
import { toHeatmap } from '../utils/transform'

/** 捕获组件注册的 ResizeObserver 回调，由用例手动喂入容器尺寸。 */
let roCallback: ResizeObserverCallback | null = null

beforeEach(() => {
  roCallback = null
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(cb: ResizeObserverCallback) {
        roCallback = cb
      }
      observe() {}
      disconnect() {}
    },
  )
})

/** 近 26 周数据：27 列 × 7 行。 */
const data = toHeatmap([{ date: '2026-09-01', total_tokens: 1234 }], 'daily', new Date('2026-09-01T12:00:00'), 26)

function renderAt(w: number, h: number) {
  const utils = render(<TokenHeatmap data={data} />)
  act(() => {
    roCallback?.([{ contentRect: { width: w, height: h } } as unknown as ResizeObserverEntry], {} as ResizeObserver)
  })
  const svg = utils.container.querySelector('svg')!
  const rect = svg.querySelector('rect')!
  return {
    ...utils,
    svg,
    svgW: Number(svg.getAttribute('width')),
    svgH: Number(svg.getAttribute('height')),
    cell: Number(rect.getAttribute('width')),
  }
}

describe('TokenHeatmap 自适应尺寸', () => {
  it('宽度充裕时格子撑满容器宽度', () => {
    const { svgW, svgH, cell } = renderAt(900, 250)
    expect(cell).toBeGreaterThan(20)
    expect(svgW).toBeLessThanOrEqual(900)
    expect(svgW).toBeGreaterThan(900 * 0.9)
    expect(svgH).toBeLessThanOrEqual(250)
  })

  it('容器扁宽时抬高容器以撑满宽度，抬高量有上限', () => {
    const { svgW, svgH, cell, container } = renderAt(2049, 212)
    const box = container.firstElementChild as HTMLElement
    // 请求抬高（同行卡片会被一并拉高，所以有上限）
    expect(parseInt(box.style.minHeight, 10)).toBe(300)
    expect(svgH).toBeLessThanOrEqual(300)
    expect(svgW).toBeLessThanOrEqual(2049)
    // 格子仍受高度约束，小于该宽度能容纳的尺寸
    expect(cell).toBeLessThan(Math.floor(2049 / 31))
  })

  it('容器比所需更高时格子继续放大到撑满宽度', () => {
    const { svgW, cell } = renderAt(900, 800)
    expect(svgW).toBeLessThanOrEqual(900)
    expect(svgW).toBeGreaterThan(900 * 0.95)
    expect(cell).toBeGreaterThan(20)
  })

  it('窄屏保底尺寸并允许横向滚动', () => {
    const { svgW, cell } = renderAt(200, 300)
    expect(cell).toBe(8)
    expect(svgW).toBeGreaterThan(200)
  })

  it('首帧未测量时用回退尺寸渲染', () => {
    const { container } = render(<TokenHeatmap data={data} />)
    const rect = container.querySelector('svg rect')!
    expect(Number(rect.getAttribute('width'))).toBe(10)
  })
})

describe('TokenHeatmap 悬浮明细', () => {
  it('悬浮格子显示日期与 token 数，离开后消失', () => {
    const { container, svg } = renderAt(900, 250)
    const el = container.querySelector('svg rect')!
    fireEvent.mouseOver(el, { clientX: 40, clientY: 60 })
    expect(document.body.textContent).toContain('tokens')

    fireEvent.mouseLeave(svg)
    expect(document.body.textContent).not.toContain('tokens')
  })
})
