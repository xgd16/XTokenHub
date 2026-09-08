// 数字滚动组件：数值变化时用弹簧从旧值平滑过渡到新值（tokens/耗时等实时字段）。
import { useEffect } from 'react'
import { motion, useMotionValue, useSpring, useTransform } from 'motion/react'

interface AnimatedNumberProps {
  value: number
  /** 数值格式化函数（如 compactCN、percent）。
   *  注意：会被滚动中的中间值反复调用，需为非数值字符生成不变化的纯函数。 */
  format: (n: number) => string
}

/** 弹簧式数字滚动：0 → value 起始即滚动，后续每次 value 变化平滑过渡。 */
export function AnimatedNumber({ value, format }: AnimatedNumberProps) {
  const mv = useMotionValue(0)
  const spring = useSpring(mv, { stiffness: 88, damping: 24, mass: 0.7 })
  const text = useTransform(spring, (v) => format(Math.round(v)))

  useEffect(() => {
    mv.set(value)
  }, [mv, value])

  return <motion.span>{text}</motion.span>
}
