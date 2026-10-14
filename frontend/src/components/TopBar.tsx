import { MoonOutlined, SearchOutlined, SunOutlined } from '@ant-design/icons'
import { Tooltip } from 'antd'
import { toggleThemeMode, useThemeMode } from '../theme'

/** 当前位号 + 页面名（记忆库下显示命名空间面包屑）。 */
function PlacePlate({ code, title, trail }: { code: string; title: string; trail: string[] }) {
  return (
    <div className="bar-place">
      <span className="bar-place-code mono">{code}</span>
      <h1 className="bar-place-title">{title}</h1>
      {trail.length > 0 && (
        <span className="bar-place-trail mono" aria-hidden="true">
          {trail.join(' / ')}
        </span>
      )}
    </div>
  )
}

/** 快捷键提示胶囊（⌘K / Ctrl K），仅视觉标记。 */
export function ShortcutHint() {
  const mac = typeof navigator !== 'undefined' && /mac/i.test(navigator.platform || navigator.userAgent)
  return (
    <span className="bar-shortcut mono" aria-hidden="true">
      {mac ? '⌘' : 'Ctrl'} K
    </span>
  )
}

/**
 * 顶栏右侧动作区：快速跳转触发、主题切换、实时连接状态。
 * `compact` 用于移动端——隐藏文字说明，只留图标与状态点。
 */
export function TopActions({
  onOpenJump,
  status,
  statusLabel,
  compact = false,
}: {
  onOpenJump: () => void
  status: 'open' | 'connecting' | 'closed'
  statusLabel: string
  compact?: boolean
}) {
  const mode = useThemeMode()
  const nextLabel = mode === 'dark' ? '切换到浅色主题' : '切换到深色主题'
  return (
    <div className="bar-actions">
      {!compact && (
        <button type="button" className="bar-jump" onClick={onOpenJump} aria-label="打开快速跳转">
          <SearchOutlined />
          <span className="bar-jump-label">快速跳转</span>
          <ShortcutHint />
        </button>
      )}
      {compact && (
        <button type="button" className="bar-icon" onClick={onOpenJump} aria-label="打开快速跳转">
          <SearchOutlined />
        </button>
      )}

      <Tooltip title={statusLabel}>
        <span className={`bar-live is-${status}`}>
          <span className="live-dot" aria-hidden="true" />
          {!compact && <span className="mono">{status === 'open' ? 'LIVE' : status === 'connecting' ? 'SYNC' : 'OFF'}</span>}
        </span>
      </Tooltip>

      <Tooltip title={nextLabel}>
        <button type="button" className="bar-icon" onClick={toggleThemeMode} aria-label={nextLabel}>
          {mode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
        </button>
      </Tooltip>
    </div>
  )
}

export { PlacePlate }