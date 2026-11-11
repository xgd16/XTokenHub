import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import ModelDonut from './ModelDonut'
import { getOption } from '../test/chart'
import type { DonutSlice } from '../utils/transform'

const slices: DonutSlice[] = [
  { name: 'gpt-4o', value: 600, percent: 60, color: '#3b82f6' },
  { name: 'claude', value: 400, percent: 40, color: '#22a06b' },
]

describe('ModelDonut', () => {
  it('底环 + 主环数据与配色正确，中心显示总量', () => {
    const { container } = render(<ModelDonut slices={slices} total={1000} />)
    const opt = getOption(container)
    expect(opt.series).toHaveLength(2)
    expect(opt.series[0].data[0].value).toBe(1)
    expect(opt.series[0].data[0].itemStyle.color).toBe('rgba(94, 128, 148, 0.14)')
    expect(opt.series[1].radius).toEqual(['57%', '83%'])
    expect(opt.series[1].startAngle).toBe(90)
    expect(opt.series[1].data.map((d: any) => d.name)).toEqual(['gpt-4o', 'claude'])
    expect(opt.series[1].data.map((d: any) => d.value)).toEqual([600, 400])
    expect(opt.series[1].data.map((d: any) => d.itemStyle.color)).toEqual([
      '#3b82f6',
      '#22a06b',
    ])
    expect(container.textContent).toContain('1000')
    expect(container.textContent).toContain('60%')
  })

  it('悬浮图例切换中心读数，离开后恢复总量', () => {
    const { container, getByText } = render(<ModelDonut slices={slices} total={1000} />)
    const row = getByText('gpt-4o').parentElement as HTMLElement

    fireEvent.mouseEnter(row)
    expect(container.textContent).toContain('60%')
    expect(container.textContent).toContain('600 tokens')

    fireEvent.mouseLeave(row)
    expect(container.textContent).toContain('1000')
  })

  it('空数据显示底环、0 与空态文案', () => {
    const { container } = render(<ModelDonut slices={[]} total={0} />)
    expect(container.querySelector('[data-echart]')).toBeTruthy()
    expect(container.textContent).toContain('0')
    expect(container.textContent).toContain('暂无数据')
  })

  it('加载中且无数据时显示骨架', () => {
    const { container } = render(<ModelDonut slices={[]} total={0} loading />)
    expect(container.querySelector('[data-echart]')).toBeNull()
    expect(container.querySelectorAll('.skeleton-donut-ring').length).toBe(2)
  })
})
