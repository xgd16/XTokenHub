import { describe, expect, it } from 'vitest'
import { NS_ALL, NS_NONE, excerpt, nsFilter, nsPath, nsTitle } from './memoryMeta'

describe('memoryMeta 命名空间路径与保留值', () => {
  it('nsPath 编码命名空间名，保留值原样', () => {
    expect(nsPath(NS_ALL)).toBe('/memories/all')
    expect(nsPath(NS_NONE)).toBe('/memories/none')
    expect(nsPath('设计')).toBe('/memories/%E8%AE%BE%E8%AE%A1')
  })

  it('nsTitle 展示名', () => {
    expect(nsTitle(NS_ALL)).toBe('全部记忆')
    expect(nsTitle(NS_NONE)).toBe('未分类')
    expect(nsTitle('pay')).toBe('pay')
  })

  it('nsFilter 过滤值（all → undefined 表示不过滤）', () => {
    expect(nsFilter(NS_ALL)).toBeUndefined()
    expect(nsFilter(NS_NONE)).toBe('none')
    expect(nsFilter('pay')).toBe('pay')
  })

  it('excerpt 取首个非空行并截断 60 字', () => {
    expect(excerpt({ content: '\n第一行摘要\n第二行' })).toBe('第一行摘要')
    expect(excerpt({ content: 'x'.repeat(80) })).toBe(`${'x'.repeat(60)}…`)
  })
})
