import { act, render, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import ModelTrendChart from './ModelTrendChart'
import { getChart, getOption } from '../test/chart'
import { MODEL_COLORS, type ModelTrendData } from '../utils/transform'

const data: ModelTrendData = {
  dates: ['2026-09-01', '2026-09-02', '2026-09-03'],
  series: [
    { name: 'gpt-4o', values: [10, 20, 0] },
    { name: 'claude', values: [5, 0, 15] },
  ],
}

describe('ModelTrendChart', () => {
  it('每个模型一条平滑折线，颜色按 MODEL_COLORS 顺序', () => {
    const { container } = render(<ModelTrendChart data={data} />)
    const opt = getOption(container)
    expect(opt.series).toHaveLength(2)
    expect(opt.series.map((s: any) => s.name)).toEqual(['gpt-4o', 'claude'])
    expect(opt.series.map((s: any) => s.type)).toEqual(['line', 'line'])
    expect(opt.series[0].smooth).toBe(0.4)
    expect(opt.series[0].data).toEqual([10, 20, 0])
    expect(opt.series[0].lineStyle.color).toBe(MODEL_COLORS[0])
    expect(opt.series[1].lineStyle.color).toBe(MODEL_COLORS[1])
    expect(opt.yAxis[0].max).toBe(20)
    expect(opt.xAxis[0].axisLabel.formatter('2026-09-02')).toBe('9月2日')
    expect(container.textContent).toContain('gpt-4o')
    expect(container.textContent).toContain('claude')
  })

  it('showTip 列出该日有值的系列（按值降序）', async () => {
    const { container } = render(<ModelTrendChart data={data} />)
    const chart = getChart(container)
    act(() => {
      chart.dispatchAction({ type: 'showTip', seriesIndex: 0, dataIndex: 1 })
    })
    await waitFor(() => expect(container.textContent).toContain('9月2日'))
    expect(container.textContent).toContain('gpt-4o')
    expect(container.textContent).toContain('20')
  })

  it('空系列显示空态提示', () => {
    const { container } = render(<ModelTrendChart data={{ dates: data.dates, series: [] }} />)
    expect(container.querySelector('[data-echart]')).toBeTruthy()
    expect(container.textContent).toContain('暂无数据')
  })

  it('加载中且无数据时显示骨架，不初始化图表', () => {
    const { container } = render(
      <ModelTrendChart data={{ dates: data.dates, series: [] }} loading />,
    )
    expect(container.querySelector('[data-echart]')).toBeNull()
    expect(container.querySelectorAll('.skeleton-bar').length).toBe(3)
  })
})
