import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router'
import McpQuickConnect from './McpQuickConnect'
import { keyApi, type APIKey } from '../api/key'

function key(patch: Partial<APIKey> = {}): APIKey {
  return {
    id: 1,
    name: 'agent',
    key: 'sk-xt-93cc883327f5dcae8efaabeef7ae6cb5',
    status: 1,
    remark: '',
    created_at: '2026-10-14T00:00:00Z',
    updated_at: '2026-10-14T00:00:00Z',
    ...patch,
  }
}

/** 在指定 origin 下渲染（location.origin 决定命令里的地址）。 */
function renderAt(origin: string) {
  window.history.replaceState(null, '', '/memories')
  Object.defineProperty(window, 'location', {
    value: { ...window.location, origin, pathname: '/memories' },
    writable: true,
  })
  return render(
    <MemoryRouter initialEntries={['/memories']}>
      <Routes>
        <Route path="/memories" element={<McpQuickConnect />} />
        <Route path="/keys" element={<div>keys-page</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

let spyList: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  window.history.replaceState(null, '', '/memories')
  spyList = vi.spyOn(keyApi, 'list')
})

describe('McpQuickConnect 命令生成', () => {
  it('OpenCode 用 --header "K=V" 等号语法，Claude Code 用 "K: V" 冒号语法', async () => {
    // 回归点：两端 --header 语法不同，写错不报错、只表现为每个工具 401
    spyList.mockResolvedValue({ items: [key()], total: 1 } as never)
    renderAt('http://192.168.1.110:9192')

    const oc = await screen.findByText(/opencode mcp add/)
    expect(oc.textContent).toContain('--header "Authorization=Bearer sk-xt-93cc883327f5dcae8efaabeef7ae6cb5"')
    expect(oc.textContent).toContain('--url http://192.168.1.110:9192/mcp')
    expect(oc.textContent).toContain('--global')

    const cc = screen.getByText(/claude mcp add/)
    expect(cc.textContent).toContain('--header "Authorization: Bearer sk-xt-93cc883327f5dcae8efaabeef7ae6cb5"')
    expect(cc.textContent).toContain('--transport http')
    expect(cc.textContent).toContain('--scope user')
  })

  it('地址取控制台自身的 origin，用户无需手改 IP', async () => {
    spyList.mockResolvedValue({ items: [key()], total: 1 } as never)
    renderAt('http://127.0.0.1:9192')
    const oc = await screen.findByText(/opencode mcp add/)
    expect(oc.textContent).toContain('http://127.0.0.1:9192/mcp')
    expect(oc.textContent).not.toContain('192.168.1.110')
  })

  it('origin 带尾斜杠时不产生双斜杠', async () => {
    spyList.mockResolvedValue({ items: [key()], total: 1 } as never)
    renderAt('http://gw.local:9192/')
    const oc = await screen.findByText(/opencode mcp add/)
    expect(oc.textContent).toContain('http://gw.local:9192/mcp')
    expect(oc.textContent).not.toContain('9192//mcp')
  })

  it('两条命令的 server 名一致，工具名才能跨端对齐', async () => {
    spyList.mockResolvedValue({ items: [key()], total: 1 } as never)
    renderAt('http://127.0.0.1:9192')
    const oc = await screen.findByText(/opencode mcp add/)
    const cc = screen.getByText(/claude mcp add/)
    expect(oc.textContent).toContain('xtokenhub-memory')
    expect(cc.textContent).toContain('xtokenhub-memory')
  })

  it('只展示启用的密钥，默认取最近更新的那条', async () => {
    spyList.mockResolvedValue({
      items: [
        key({ id: 1, name: 'old', key: 'sk-xt-old', updated_at: '2026-10-01T00:00:00Z' }),
        key({ id: 2, name: 'disabled', key: 'sk-xt-off', status: 0, updated_at: '2026-10-20T00:00:00Z' }),
        key({ id: 3, name: 'new', key: 'sk-xt-new', updated_at: '2026-10-10T00:00:00Z' }),
      ],
      total: 3,
    } as never)
    renderAt('http://127.0.0.1:9192')
    const oc = await screen.findByText(/opencode mcp add/)
    expect(oc.textContent).toContain('sk-xt-new')
    expect(oc.textContent).not.toContain('sk-xt-old')
    expect(oc.textContent).not.toContain('sk-xt-off')
  })

  it('多把可用密钥时给出选择器', async () => {
    spyList.mockResolvedValue({
      items: [key({ id: 1, name: 'a' }), key({ id: 2, name: 'b', key: 'sk-xt-b', updated_at: '2026-10-11T00:00:00Z' })],
      total: 2,
    } as never)
    renderAt('http://127.0.0.1:9192')
    await screen.findByText(/opencode mcp add/)
    expect(screen.getByText('使用密钥')).toBeTruthy()
  })

  it('没有启用密钥时提示去创建，而不是给出无效命令', async () => {
    spyList.mockResolvedValue({ items: [key({ status: 0 })], total: 1 } as never)
    renderAt('http://127.0.0.1:9192')
    await waitFor(() => expect(screen.getByText(/还没有启用的 API Key/)).toBeTruthy())
    expect(screen.queryByText(/opencode mcp add/)).toBeNull()
    expect(screen.getByText('去创建')).toBeTruthy()
  })

  it('拉取密钥失败时不阻塞页面，退化为无命令提示', async () => {
    spyList.mockRejectedValue(new Error('boom'))
    renderAt('http://127.0.0.1:9192')
    await waitFor(() => expect(screen.getByText(/还没有启用的 API Key/)).toBeTruthy())
  })

  it('命令块可聚焦，键盘也能触发复制', async () => {
    spyList.mockResolvedValue({ items: [key()], total: 1 } as never)
    renderAt('http://127.0.0.1:9192')
    const oc = await screen.findByText(/opencode mcp add/)
    expect(oc.getAttribute('role')).toBe('button')
    expect(oc.getAttribute('tabindex')).toBe('0')
  })
})