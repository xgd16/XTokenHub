import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnimatedNumber } from './AnimatedNumber'

// 弹簧动画在 jsdom 里不会自行收敛：把 motion 换成恒等实现，
// 让 value 原样进入 format，直接断言「中间值/终值怎么格式化」。
vi.mock('motion/react', () => ({
  motion: { span: ({ children }: { children?: unknown }) => <span>{children as never}</span> },
  useMotionValue: () => ({ set: vi.fn() }),
  useSpring: () => ({ get: () => 2.3989 }),
  useTransform: (_mv: unknown, fn: (v: number) => string) => fn(2.3989),
}))

describe('AnimatedNumber', () => {
  // 金额是小数美元：组件若先 Math.round 再交给换汇，$2.3989 会显示成 ¥14.400（= $2 × 7.2），
  // 与 mac 端/接口原值差 3 元。取整必须由各 format 自己决定。
  it('不预先取整，金额保留小数精度', () => {
    const money = (usd: number) => `¥${(usd * 7.2).toFixed(3)}`
    const { container } = render(<AnimatedNumber value={2.3989} format={money} />)
    expect(container.textContent).toBe('¥17.272')
  })

  it('需要整数的 format 自己取整', () => {
    const streak = (n: number) => `${Math.round(n)} 天`
    const { container } = render(<AnimatedNumber value={2.3989} format={streak} />)
    expect(container.textContent).toBe('2 天')
  })
})
