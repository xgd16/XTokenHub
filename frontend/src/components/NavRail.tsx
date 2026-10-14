import { useEffect, useState } from 'react'
import { Popover, Tooltip } from 'antd'
import { MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons'
import { isActive, navLeaves, activePath, type NavGroup, type NavLeaf } from '../layouts/nav'
import HubMark from './HubMark'

interface RailProps {
  groups: NavGroup[]
  pathname: string
  collapsed: boolean
  onNavigate: (path: string) => void
  onToggleCollapse: () => void
  /** 底部按钮文案（抽屉里是「关闭」，桌面是「收起」）。 */
  toggleLabel?: string
}

/** 导航行：位号 / 图标 / 标题，hover 时右侧浮出「跳转」微提示。 */
function RailRow({
  leaf,
  active,
  collapsed,
  onNavigate,
}: {
  leaf: NavLeaf
  active: boolean
  collapsed: boolean
  onNavigate: (path: string) => void
}) {
  const row = (
    <button
      type="button"
      className={`rail-row${active ? ' is-active' : ''}`}
      aria-current={active ? 'page' : undefined}
      onClick={() => onNavigate(leaf.path)}
    >
      <span className="rail-row-glyph">{leaf.icon}</span>
      {!collapsed && <span className="rail-row-label">{leaf.label}</span>}
      {!collapsed && leaf.code && <span className="rail-row-code mono">{leaf.code}</span>}
    </button>
  )
  if (!collapsed) return row
  return (
    <Tooltip title={leaf.label} placement="right" mouseEnterDelay={0.15}>
      {row}
    </Tooltip>
  )
}

/** 命名空间子树：父项 + 子项折叠区。 */
function RailBranch({
  node,
  pathname,
  active,
  collapsed,
  onNavigate,
}: {
  node: NavGroup['nodes'][number]
  pathname: string
  /** 已解析的当前叶子路径（最长前缀），子项据此点亮，避免父子同时高亮 */
  active: string
  collapsed: boolean
  onNavigate: (path: string) => void
}) {
  const branchActive = isActive(pathname, node.path)
  const childActive = (node.children ?? []).some((c) => c.path === active)
  // 默认展开：命名空间是记忆库的主要入口，收起会让它难以发现
  const [open, setOpen] = useState(true)

  // 用户手动收起后，若当前正处于某个子项（如从外部深链进来），重新展开
  useEffect(() => {
    if (childActive) setOpen(true)
  }, [childActive])

  const parent = (
    <button
      type="button"
      className={`rail-row rail-row-parent${branchActive ? ' is-active' : ''}`}
      aria-expanded={open}
      onClick={() => {
        // 折叠态没有子树可展开，父项改为直达其所属页（记忆库 → 命名空间管理）
        if (collapsed) {
          onNavigate(node.path)
          return
        }
        setOpen((v) => !v)
      }}
    >
      <span className="rail-row-glyph">{node.icon}</span>
      {!collapsed && <span className="rail-row-label">{node.label}</span>}
      {!collapsed && (
        <span className={`rail-caret${open ? ' is-open' : ''}`} aria-hidden="true">
          <svg viewBox="0 0 12 12" width="10" height="10">
            <path d="M4 2.5 8 6l-4 3.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
      )}
    </button>
  )

  // 折叠态：父项直达所属页，命名空间列表交给「⋯」按钮弹出的 Popover
  if (collapsed) {
    return (
      <Tooltip title={node.label} placement="right" mouseEnterDelay={0.15}>
        <span className="rail-branch-collapsed">
          {parent}
          <Popover
            placement="right"
            trigger="click"
            overlayClassName="rail-pop"
            content={
              <div className="rail-pop-list">
                {(node.children ?? []).map((c) => (
                  <RailRow
                    key={c.path}
                    leaf={c}
                    collapsed={false}
                    active={c.path === active}
                    onNavigate={onNavigate}
                  />
                ))}
              </div>
            }
          >
            <button
              type="button"
              className={`rail-fold${branchActive ? ' is-active' : ''}`}
              aria-label={`展开${node.label}命名空间`}
            >
              <span aria-hidden="true">⋯</span>
            </button>
          </Popover>
        </span>
      </Tooltip>
    )
  }

  return (
    <div className="rail-branch">
      {parent}
      {open && (
        <div className="rail-sub">
          {(node.children ?? []).map((c) => (
            <button
              key={c.path}
              type="button"
              className={`rail-sub-row${c.path === active ? ' is-active' : ''}`}
              aria-current={c.path === active ? 'page' : undefined}
              onClick={() => onNavigate(c.path)}
            >
              <span className="rail-sub-dot" aria-hidden="true" />
              <span className="rail-sub-label">{c.label}</span>
              {c.count !== undefined && <span className="rail-sub-count mono">{c.count}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/**
 * 控制台导航轨：品牌位 → 分组微标题 → 导航行 → 记忆库子树。
 * 折叠态只留图标与 tooltip，子树改由 Popover 承载。
 */
export default function NavRail({
  groups,
  pathname,
  collapsed,
  onNavigate,
  onToggleCollapse,
  toggleLabel = '收起',
}: RailProps) {
  const active = activePath(pathname, navLeaves(groups))

  return (
    <div className={`rail${collapsed ? ' is-collapsed' : ''}`}>
      <button
        type="button"
        className="rail-brand"
        onClick={() => onNavigate('/')}
        aria-label="回到仪表盘"
        title={collapsed ? 'XTokenHub' : undefined}
      >
        <span className="rail-mark">
          <HubMark size={19} />
        </span>
        {!collapsed && (
          <span className="rail-brand-text">
            <span className="rail-brand-name">XTokenHub</span>
            <span className="rail-brand-sub mono">AI GATEWAY</span>
          </span>
        )}
      </button>

      <nav className="rail-nav" aria-label="主导航">
        {groups.map((g) => (
          <div className="rail-group" key={g.code}>
            {!collapsed && (
              <div className="rail-group-head">
                <span className="rail-group-label">{g.label}</span>
                <span className="rail-group-code mono">{g.code}</span>
              </div>
            )}
            {g.nodes.map((n) =>
              n.children ? (
                <RailBranch
                  key={n.path}
                  node={n}
                  pathname={pathname}
                  active={active}
                  collapsed={collapsed}
                  onNavigate={onNavigate}
                />
              ) : (
                <RailRow
                  key={n.path}
                  leaf={n}
                  collapsed={collapsed}
                  active={active === n.path}
                  onNavigate={onNavigate}
                />
              ),
            )}
          </div>
        ))}
      </nav>

      <button
        type="button"
        className="rail-collapse"
        onClick={onToggleCollapse}
        aria-label={collapsed ? '展开导航' : toggleLabel}
      >
        {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
        {!collapsed && <span>{toggleLabel}</span>}
      </button>
    </div>
  )
}