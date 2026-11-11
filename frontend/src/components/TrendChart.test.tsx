import { act, render, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import TrendChart from './TrendChart'
import { getChart, getOption } from '../test/chart'
import type { SeriesPoint } from '../utils/transform'

const pts: SeriesPoint[] = [
  { date: '2026-09-01', requests: 0, errors: 0, tokens: 0 },
  { date: '2026-09-02', requests: 14, errors: 0, tokens: 0 },
  { date: '2026-09-03', requests: 3, errors: 1, tokens: 0 },
]

describe('TrendChart', () => {
  it('渲染折线、面积与峰值标注', () => {
    const { container } = render(<TrendChart data={pts} />)
    const opt = getOption(container)
    expect(opt.series[0].type).toBe('line')
    expect(opt.series[0].data).toEqual([0, 14, 3])
    expect(opt.series[0].areaStyle).toBeTruthy()
    expect(opt.series[0].markPoint.data[0].coord).toEqual([1, 14])
    expect(opt.series[0].markPoint.label.formatter).toBe('14')
    expect(opt.xAxis[0].data).toEqual(['2026-09-01', '2026-09-02', '2026-09-03'])
    expect(opt.xAxis[0].axisLabel.formatter('2026-09-02')).toBe('09-02')
    expect(container.querySelector('[role="img"]')?.getAttribute('aria-label')).toBe('请求趋势')
  })

  it('全 0 数据仍渲染图表并显示空态提示', () => {
    const zeros = pts.map((p) => ({ ...p, requests: 0 }))
    const { container } = render(<TrendChart data={zeros} />)
    expect(container.querySelector('[data-echart]')).toBeTruthy()
    expect(container.textContent).toContain('暂无数据')
  })

  it('空数组渲染占位文案', () => {
    const { container } = render(<TrendChart data={[]} />)
    expect(container.querySelector('[data-echart]')).toBeNull()
    expect(container.textContent).toContain('暂无数据')
  })
})

describe('TrendChart 悬浮交互', () => {
  it('showTip 出现明细提示，hideTip 后消失', async () => {
    const { container } = render(<TrendChart data={pts} />)
    const chart = getChart(container)
    const tooltip = () =>
      Array.from(container.querySelectorAll<HTMLElement>('div')).find(
        (el) => el.style.position === 'absolute' && el.textContent?.includes('请求数 14'),
      )

    act(() => {
      chart.dispatchAction({ type: 'showTip', seriesIndex: 0, dataIndex: 1 })
    })
    await waitFor(() => expect(tooltip()?.style.display).not.toBe('none'))
    expect(tooltip()?.textContent).toContain('2026-09-02')

    act(() => {
      chart.dispatchAction({ type: 'hideTip' })
    })
    await waitFor(() => expect(tooltip()?.style.display).toBe('none'))
  })
})
