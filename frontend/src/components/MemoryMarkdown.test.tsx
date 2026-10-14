import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { MemoryMarkdown } from './MemoryMarkdown'

describe('MemoryMarkdown', () => {
  it('渲染标题 / 列表 / 行内代码 / 表格', () => {
    render(
      <MemoryMarkdown
        content={'## 部署流程\n\n- 甲\n- 乙\n\n`code`\n\n| 列 | 值 |\n| --- | --- |\n| a | 1 |'}
      />,
    )
    expect(screen.getByRole('heading', { level: 2, name: '部署流程' })).toBeTruthy()
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('code')).toBeTruthy()
    expect(screen.getByRole('table')).toBeTruthy()
  })

  it('不渲染原始 HTML（防注入）', () => {
    const { container } = render(
      <MemoryMarkdown content={'<img src=x onerror=alert(1)>\n\n**安全**'} />,
    )
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByText('安全')).toBeTruthy()
  })
})
