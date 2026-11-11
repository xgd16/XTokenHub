// 数字滚动组件：数值变化时用弹簧从旧值平滑过渡到新值（tokens/耗时等实时字段）。
import { useEffect } from 'react'
import { motion, useMotionValue, useSpring, useTransform } from 'motion/react'

interface AnimatedNumberProps {
  value: number
  /** 数值格式化函数（如 compactCN、money）。
   *  注意：会被滚动中的中间值反复调用，必须自己处理小数（需要整数就在函数里 round），
   *  且对同一数值必须稳定返回，否则数字会在滚动中抖动。 */
  format: (n: number) => string
}

/** 弹簧式数字滚动：0 → value 起始即滚动，后续每次 value 变化平滑过渡。
 *
 *  外层用目标值文本（data-reserve）撑住盒宽，样式见 global.css 的 .animated-number：
 *  补间过程每帧的字符数都不同（0 → 1 → … → 9999 → 1万 → 67.58万），若宽度跟着
 *  实时文本走，读数所在的 max-content 栅格每帧重排，整行读数跟着抖。 */
export function AnimatedNumber({ value, format }: AnimatedNumberProps) {
  const mv = useMotionValue(0)
  const spring = useSpring(mv, { stiffness: 88, damping: 24, mass: 0.7 })
  // 不在这里取整：金额是小数美元，先 round 再换汇会把 $2.39 显示成 ¥14.40（= $2 × 7.2）
  const text = useTransform(spring, (v) => format(v))

  useEffect(() => {
    mv.set(value)
  }, [mv, value])

  return (
    <span className="animated-number" data-reserve={format(value)}>
      <motion.span>{text}</motion.span>
    </span>
  )
}
