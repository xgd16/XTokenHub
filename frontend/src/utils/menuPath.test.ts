import { describe, expect, it } from 'vitest'
import { bestMenuPath } from './menuPath'

const PATHS = [
  '/channels',
  '/memories',
  '/memories/all',
  '/memories/none',
  '/memories/%E6%B5%8B%E8%AF%95',
  '/settings',
]

describe('bestMenuPath', () => {
  it('子路径优先于父级路径', () => {
    expect(bestMenuPath('/memories/all', PATHS)).toBe('/memories/all')
    expect(bestMenuPath('/memories/%E6%B5%8B%E8%AF%95', PATHS)).toBe('/memories/%E6%B5%8B%E8%AF%95')
    expect(bestMenuPath('/memories', PATHS)).toBe('/memories')
  })

  it('不误匹配同前缀的其他路径（按段边界）', () => {
    expect(bestMenuPath('/memories-foo', PATHS)).toBe('/')
    expect(bestMenuPath('/settings2', PATHS)).toBe('/')
  })

  it('无匹配回退根路径', () => {
    expect(bestMenuPath('/', PATHS)).toBe('/')
    expect(bestMenuPath('/unknown', PATHS)).toBe('/')
  })
})
