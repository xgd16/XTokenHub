import { echarts, type EChartsType } from '../charts/echarts'

/** 测试里从 EChart 宿主取回 ECharts 实例（组件内部不暴露 ref）。 */
export function getChart(container: HTMLElement): EChartsType {
  const host = container.querySelector<HTMLElement>('[data-echart]')
  if (!host) throw new Error('找不到 ECharts 宿主节点')
  const chart = echarts.getInstanceByDom(host)
  if (!chart) throw new Error('ECharts 实例未初始化')
  return chart
}

/** getOption() 的结构类型很宽，测试里按需取用。 */
export function getOption(container: HTMLElement): any {
  return getChart(container).getOption()
}
