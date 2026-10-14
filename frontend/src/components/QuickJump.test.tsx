import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import QuickJump from './QuickJump'
import { buildNav } from '../layouts/nav'

const groups = buildNav([
  { id: 1, name: 'xtokenhub', description: '', created_by: 'manual', count: 9 },
])

function setup() {
  const onClose = vi.fn()
  const onNavigate = vi.fn()
  render(<QuickJump groups={groups} onClose={onClose} onNavigate={onNavigate} />)
  return { onClose, onNavigate, input: screen.getByLabelText('搜索导航') as HTMLInputElement }
}

describe('QuickJump', () => {
  it('默认聚焦输入框并列出全部入口', () => {
    const { input } = setup()
    expect(document.activeElement).toBe(input)
    expect(screen.getAllByRole('button', { name: /仪表盘|渠道管理/ }).length).toBeGreaterThan(0)
  })

  it('输入过滤后 Enter 打开高亮项并关闭', () => {
    const { input, onNavigate, onClose } = setup()
    fireEvent.change(input, { target: { value: '渠道' } })
    fireEvent.keyDown(window, { key: 'Enter' })
    expect(onNavigate).toHaveBeenCalledWith('/channels')
    expect(onClose).toHaveBeenCalled()
  })

  it('hover 某行后回车命中该行', () => {
    const { input, onNavigate } = setup()
    fireEvent.change(input, { target: { value: '记忆' } })
    const rows = screen.getAllByRole('button').filter((b) => b.className.includes('jump-row'))
    expect(rows[0].textContent).toContain('全部记忆')
    expect(rows[1].textContent).toContain('未分类')
    fireEvent.mouseEnter(rows[1])
    fireEvent.keyDown(window, { key: 'Enter' })
    expect(onNavigate).toHaveBeenCalledWith('/memories/none')
  })

  it('Esc 关闭；无匹配时给出空态', () => {
    const { input, onClose } = setup()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
    fireEvent.change(input, { target: { value: 'zzz' } })
    expect(screen.getByText(/没有匹配/)).toBeTruthy()
  })
})