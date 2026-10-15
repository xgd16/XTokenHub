import { describe, expect, it } from 'vitest'
import { pendingLinksToInput, type PendingMemoryLink } from './memory'

const p = (to_id: number, relation: PendingMemoryLink['relation']): PendingMemoryLink => ({
  to_id, relation, title: `t${to_id}`,
})

describe('pendingLinksToInput', () => {
  it('空列表给出空 links/supersedes（不选关系也能正常写入）', () => {
    expect(pendingLinksToInput([])).toEqual({ links: [], supersedes: [] })
  })

  it('替代走 supersedes，其余走 links', () => {
    const out = pendingLinksToInput([
      p(1, 'related'), p(2, 'conflicts'), p(3, 'derived_from'), p(4, 'supersedes'),
    ])
    expect(out.supersedes).toEqual([4])
    expect(out.links).toEqual([
      { to_id: 1, relation: 'related' },
      { to_id: 2, relation: 'conflicts' },
      { to_id: 3, relation: 'derived_from' },
    ])
  })

  it('替代不能落到 links：只发 links 不会翻转旧记忆状态', () => {
    // 后端 Supersede 同时建 supersedes 边 + 把旧记忆置为 superseded，
    // 普通 link 只建边。混进 links 会得到「有边但状态没变」的半截结果。
    const out = pendingLinksToInput([p(7, 'supersedes')])
    expect(out.links.some((l) => l.relation === 'supersedes')).toBe(false)
    expect(out.supersedes).toContain(7)
  })

  it('同一目标只保留第一条（后端建边幂等，但列表会出重复行）', () => {
    const out = pendingLinksToInput([p(1, 'related'), p(1, 'conflicts')])
    expect(out.links).toEqual([{ to_id: 1, relation: 'related' }])
    expect(out.supersedes).toEqual([])
  })

  it('不把 title 传给后端（只用于提交前展示）', () => {
    const out = pendingLinksToInput([p(1, 'related')])
    expect(out.links[0]).not.toHaveProperty('title')
    expect(out.links[0]).not.toHaveProperty('note')
  })
})
