import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { PageHero } from './PageHero'

const readouts = [{ label: '今日请求', value: '67' }]

describe('PageHero', () => {
  it('未传 aside 时保持原结构：foot 与 inner 同级横跨整行', () => {
    const { container } = render(
      <PageHero eyebrow="obs" title="标题" readouts={readouts} foot={<span>状态条</span>} />,
    )
    expect(container.querySelector('.obs-hero-split')).toBeNull()
    const section = container.querySelector('.obs-hero')!
    expect(section.querySelector(':scope > .obs-hero-inner')).not.toBeNull()
    expect(section.querySelector(':scope > .obs-hero-foot')).not.toBeNull()
  })

  it('传 aside 时切分栏，且 foot 被收进左栏、不再横跨右栏', () => {
    // 回归点：foot 曾是整行，右侧面板下方被白占一整行高度，撑不满 hero 右半边
    const { container } = render(
      <PageHero
        eyebrow="obs"
        title="标题"
        readouts={readouts}
        aside={<div className="obs-pulse">面板</div>}
        foot={<span>状态条</span>}
      />,
    )
    const split = container.querySelector('.obs-hero-split')!
    expect(split).not.toBeNull()
    const main = split.querySelector('.obs-hero-main')!
    // foot 在左栏内，与 inner 同级；右栏只有面板
    expect(main.querySelector(':scope > .obs-hero-foot')).not.toBeNull()
    expect(main.querySelector(':scope > .obs-hero-inner')).not.toBeNull()
    expect(split.querySelector(':scope > .obs-hero-foot')).toBeNull()
    expect(split.querySelector(':scope > .obs-hero-aside-wrap > .obs-pulse')).not.toBeNull()
  })

  it('aside 存在但无 foot 时左栏不渲染空的 foot 占位', () => {
    const { container } = render(<PageHero eyebrow="obs" title="标题" aside={<div className="obs-pulse">面板</div>} />)
    expect(container.querySelector('.obs-hero-foot')).toBeNull()
  })

  it('没有 readouts 时不渲染读数栅格', () => {
    const { container } = render(<PageHero eyebrow="obs" title="标题" />)
    expect(container.querySelector('.obs-readouts')).toBeNull()
  })

  it('slim 模式把 slim 类挂到 section 上', () => {
    const { container } = render(<PageHero eyebrow="obs" title="标题" slim />)
    expect(container.querySelector('.obs-hero')!.className).toContain('slim')
  })
})