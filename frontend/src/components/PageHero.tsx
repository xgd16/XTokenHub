import type { CSSProperties, ReactNode } from 'react'

/** 读数项：大号等宽数字 + 小标签（hero 内的关键指标）。 */
export interface ObsReadoutItem {
  label: string
  value: ReactNode
  /** 语义着色：accent=电光绿（主指标）/ warn=琥珀（待处理）/ cyan=青（实时类）。 */
  tone?: 'accent' | 'warn' | 'cyan'
}

interface PageHeroProps {
  /** 眉题（等宽大写）：如 "channels · 上游接入"。 */
  eyebrow: string
  title: ReactNode
  /** 标题下的一句话说明（可选）。 */
  desc?: ReactNode
  readouts?: ObsReadoutItem[]
  /** 右侧操作区。 */
  actions?: ReactNode
  /** 右侧内容区（面板 / 速览表等），与 actions 同列、底对齐。 */
  aside?: ReactNode
  /** 底部状态条：实时状态 / 清理进度 / 分类进度等（可选）。 */
  foot?: ReactNode
  /** 装饰层（绝对定位底纹，如记忆星图星座）。 */
  backdrop?: ReactNode
  /** 紧凑模式（二级页标题栏）。 */
  slim?: boolean
  style?: CSSProperties
}

/**
 * 页级观测台 Hero：所有页面统一的头部结构（眉题 → 标题 → 读数 → 操作 → 状态条）。
 * 视觉规则见 styles/console.css 的 .obs-hero。
 *
 * 给了 aside 时切换成左右分栏（.obs-hero-split）：状态条 foot 只归左侧一栏，
 * 右侧面板从顶部一路到底。若仍让 foot 整行横跨，面板下方就白占一整行高度——
 * 这正是「右侧该占满却被挤矮」的原因。
 */
export function PageHero({ eyebrow, title, desc, readouts, actions, aside, foot, backdrop, slim, style }: PageHeroProps) {
  const main = (
    <>
      <div className="obs-eyebrow">{eyebrow}</div>
      <h2 className="obs-title">{title}</h2>
      {desc && <div className="obs-desc">{desc}</div>}
      {readouts && readouts.length > 0 && (
        <div className="obs-readouts">
          {readouts.map((r) => (
            <div className="obs-readout" key={r.label}>
              <span className={`obs-readout-num${r.tone ? ` ${r.tone}` : ''}`}>{r.value}</span>
              <span className="obs-readout-label">{r.label}</span>
            </div>
          ))}
        </div>
      )}
    </>
  )

  return (
    <section className={`obs-hero${slim ? ' slim' : ''}`} style={style}>
      {backdrop}
      {aside ? (
        <div className="obs-hero-split">
          <div className="obs-hero-main">
            <div className="obs-hero-inner">
              <div>{main}</div>
              {actions && <div className="obs-hero-actions">{actions}</div>}
            </div>
            {foot && <div className="obs-hero-foot">{foot}</div>}
          </div>
          <div className="obs-hero-aside-wrap">{aside}</div>
        </div>
      ) : (
        <>
          <div className="obs-hero-inner">
            <div>{main}</div>
            {actions && <div className="obs-hero-actions">{actions}</div>}
          </div>
          {foot && <div className="obs-hero-foot">{foot}</div>}
        </>
      )}
    </section>
  )
}
