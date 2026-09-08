// 共享入场动画变体与工具（基于 motion/react）。
import type { CSSProperties, ReactNode } from 'react'
import { motion, type Variants } from 'motion/react'

/** 全局统一缓动：轻微过冲的 экспо缓动，营造「起落」质感。 */
export const easeOutExpo: [number, number, number, number] = [0.16, 1, 0.3, 1]

/** 上浮渐显变体（供容器 staggerChildren 使用）。 */
export const fadeUp: Variants = {
  hidden: { opacity: 0, y: 14 },
  show: { opacity: 1, y: 0, transition: { duration: 0.55, ease: easeOutExpo } },
}

/** 容器 stagger：子项逐个上浮入场。 */
export const staggerContainer: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.09, delayChildren: 0.05 } },
}

interface RevealProps {
  children: ReactNode
  /** 进入延迟（秒），用于分段步进。 */
  delay?: number
  /** 初始水平偏移（px）。 */
  x?: number
  /** 初始垂直偏移（px）。 */
  y?: number
  className?: string
  style?: CSSProperties
}

/** 单个区块进入：上浮渐显，支持延迟步进（用于整列分段入场）。 */
export function Reveal({ children, delay = 0, x = 0, y = 14, className, style }: RevealProps) {
  return (
    <motion.div
      initial={{ opacity: 0, x, y }}
      animate={{ opacity: 1, x: 0, y: 0 }}
      transition={{ duration: 0.55, delay, ease: easeOutExpo }}
      className={className}
      style={style}
    >
      {children}
    </motion.div>
  )
}
