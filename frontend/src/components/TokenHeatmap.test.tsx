import { act, render, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TokenHeatmap from './TokenHeatmap'
import { toHeatmap } from '../utils/transform'
import { computeHeatmapLayout } from '../charts/heatmapLayout'
import { getChart, getOption } from '../test/chart'

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
      unobserve() {}
      disconnect() {}
    },
  )
})

/** 近 26 周数据：27 列 × 7 行。 */
const data = toHeatmap([{ date: '2026-09-01', total_tokens: 1234 }], 'daily', new Date('2026-09-01T12:00:00'), 26)
const cols = data.columns.length
const rows = Math.max(...data.columns.map((c) => c.cells.length), 1)
const totalCells = data.columns.reduce((n, c) => n + c.cells.length, 0)

function renderAt(w: number, h: number) {
  const utils = render(<TokenHeatmap data={data} />)
  act(() => {
    roCallback?.([{ contentRect: { width: w, height: h } } as unknown as ResizeObserverEntry], {} as ResizeObserver)
  })
  const host = utils.container.querySelector<HTMLElement>('[data-echart]')!
  return {
    ...utils,
    host,
    chartW: Number.parseFloat(host.style.width),
    chartH: Number.parseFloat(host.style.height),
    cell: Number((getOption(utils.container).series[0].symbolSize as number[])[0]),
  }
}

describe('TokenHeatmap 自适应布局（纯函数）', () => {
  it('宽度充裕时格子撑满容器宽度', () => {
    const geo = computeHeatmapLayout(cols, rows, 900, 250)
    expect(geo.cell).toBeGreaterThan(20)
    expect(geo.W).toBeLessThanOrEqual(900)
    expect(geo.W).toBeGreaterThan(900 * 0.9)
    expect(geo.H).toBeLessThanOrEqual(250)
  })

  it('容器扁宽时抬高容器以撑满宽度，抬高量有上限', () => {
    const geo = computeHeatmapLayout(cols, rows, 2049, 212)
    expect(geo.boxMinH).toBe(300)
    expect(geo.H).toBeLessThanOrEqual(300)
    expect(geo.W).toBeLessThanOrEqual(2049)
    // 格子仍受高度约束，小于该宽度能容纳的尺寸
    expect(geo.cell).toBeLessThan(Math.floor(2049 / 31))
  })

  it('容器比所需更高时格子继续放大到撑满宽度', () => {
    const geo = computeHeatmapLayout(cols, rows, 900, 800)
    expect(geo.W).toBeLessThanOrEqual(900)
    expect(geo.W).toBeGreaterThan(900 * 0.95)
    expect(geo.cell).toBeGreaterThan(20)
  })

  it('窄屏保底尺寸并允许横向滚动', () => {
    const geo = computeHeatmapLayout(cols, rows, 200, 300)
    expect(geo.cell).toBe(8)
    expect(geo.W).toBeGreaterThan(200)
  })

  it('首帧未测量时用回退尺寸渲染', () => {
    const geo = computeHeatmapLayout(cols, rows, null, null)
    expect(geo.cell).toBe(10)
  })
})

describe('TokenHeatmap 组件', () => {
  it('按容器尺寸初始化 ECharts 画布并渲染全部格子', () => {
    const { container, chartW, chartH, cell } = renderAt(900, 250)
    const opt = getOption(container)
    expect(opt.series[0].type).toBe('scatter')
    expect(opt.series[0].data).toHaveLength(totalCells)
    expect(chartW).toBeLessThanOrEqual(900)
    expect(chartW).toBeGreaterThan(900 * 0.9)
    expect(chartH).toBeLessThanOrEqual(250)
    expect(cell).toBeGreaterThan(19)
    expect(container.querySelector('[role="img"]')?.getAttribute('aria-label')).toBe('Token 活动热力图')
  })

  it('容器扁宽时把 minHeight 抬到 300', () => {
    const { container } = renderAt(2049, 212)
    const box = container.firstElementChild as HTMLElement
    expect(Number.parseInt(box.style.minHeight, 10)).toBe(300)
  })

  it('窄屏保底尺寸并保留横向滚动宽度', () => {
    const { chartW } = renderAt(200, 300)
    expect(chartW).toBeGreaterThan(200)
  })

  it('首帧未测量时用回退尺寸渲染', () => {
    const { chartW, cell } = renderAt(0, 0)
    expect(chartW).toBe(computeHeatmapLayout(cols, rows, null, null).W)
    expect(cell).toBeGreaterThan(9)
    expect(cell).toBeLessThan(10)
  })

  it('showTip 显示日期与 token 数，hideTip 后隐藏', async () => {
    const { container } = render(<TokenHeatmap data={data} />)
    const chart = getChart(container)
    const first = data.columns[0].cells[0]
    const label = `${Number(first.date.slice(5, 7))}月${Number(first.date.slice(8, 10))}日`
    const tooltip = () =>
      Array.from(document.querySelectorAll<HTMLElement>('div')).find(
        (el) => el.style.position === 'absolute' && el.textContent?.includes(label),
      )

    act(() => {
      chart.dispatchAction({ type: 'showTip', seriesIndex: 0, dataIndex: 0 })
    })
    expect(tooltip()?.style.display).not.toBe('none')
    expect(tooltip()?.textContent).toContain('tokens')

    act(() => {
      chart.dispatchAction({ type: 'hideTip' })
    })
    await waitFor(() => expect(tooltip()?.style.display).toBe('none'))
  })
})
