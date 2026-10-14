import { describe, expect, it } from 'vitest'
import { activePath, buildNav, isActive, navLeaves, placeFor, searchNav } from './nav'
import { nsPath } from '../utils/memoryMeta'

const ns = [
  { id: 1, name: 'xtokenhub', description: '', created_by: 'manual', count: 9 },
  { id: 2, name: '支付', description: '', created_by: 'llm', count: 3 },
]
const groups = buildNav(ns)

describe('buildNav', () => {
  it('按观测/网关/模型/记忆分组，编号唯一', () => {
    expect(groups.map((g) => g.code)).toEqual(['OBS', 'GATE', 'MOD', 'MEM'])
    const codes = navLeaves(groups).map((l) => l.code).filter(Boolean)
    expect(new Set(codes).size).toBe(codes.length)
  })

  it('记忆库子树带命名空间与计数', () => {
    const mem = groups[3].nodes[0]
    const labels = (mem.children ?? []).map((c) => c.label)
    expect(labels).toEqual(['全部记忆', '未分类', 'xtokenhub', '支付', '命名空间管理'])
    expect(mem.children?.[2].count).toBe(9)
    expect(mem.children?.[2].path).toBe('/memories/xtokenhub')
  })
})

describe('isActive / activePath', () => {
  it('按段边界匹配，不误伤同前缀路径', () => {
    expect(isActive('/memories/xtokenhub', '/memories')).toBe(true)
    expect(isActive('/memories-foo', '/memories')).toBe(false)
    expect(isActive('/', '/')).toBe(true)
    expect(isActive('/logs', '/')).toBe(false)
  })

  it('取最长前缀叶子', () => {
    const leaves = navLeaves(groups)
    expect(activePath('/', leaves)).toBe('/')
    expect(activePath('/memories', leaves)).toBe('/memories')
    expect(activePath('/memories/xtokenhub', leaves)).toBe('/memories/xtokenhub')
    expect(activePath('/memories/不存在', leaves)).toBe('/memories')
    expect(activePath('/unknown/deep', leaves)).toBe('/')
  })
})

describe('placeFor', () => {
  it('记忆库页统一以「记忆库」为标题，命名空间下沉为面包屑', () => {
    expect(placeFor('/logs', groups)).toMatchObject({ code: '02', title: '请求日志', trail: [] })
    expect(placeFor('/memories', groups)).toMatchObject({ code: '08', title: '记忆库', trail: [] })
    expect(placeFor('/memories/xtokenhub', groups)).toMatchObject({
      code: '08',
      title: '记忆库',
      trail: ['xtokenhub'],
    })
    expect(placeFor('/memories/all', groups).trail).toEqual(['全部记忆'])
    expect(placeFor('/memories/none', groups).trail).toEqual(['未分类'])
  })
})

describe('searchNav', () => {
  it('空查询返回全部入口（保持骨架顺序）', () => {
    expect(searchNav(groups, '')).toHaveLength(navLeaves(groups).length)
    expect(searchNav(groups, '')[0].leaf.label).toBe('仪表盘')
  })

  it('标签命中优先，关键词/路径可召回', () => {
    expect(searchNav(groups, '记忆')[0].leaf.label).toBe('全部记忆')
    expect(searchNav(groups, 'key')[0].leaf.path).toBe('/keys')
    expect(searchNav(groups, '支付')[0].leaf.path).toBe(nsPath('支付'))
    expect(searchNav(groups, 'zzz')).toHaveLength(0)
  })
})