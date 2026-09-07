import { Grid } from 'antd'

/** 是否移动端视口（< md/768px）。首帧按 matchMedia 实时值返回，桌面优先。 */
export function useIsMobile(): boolean {
  const screens = Grid.useBreakpoint()
  return screens.md === false
}
