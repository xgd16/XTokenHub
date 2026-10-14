import { useEffect, useMemo, useRef, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { RightOutlined } from '@ant-design/icons'
import { searchNav, type NavGroup } from '../layouts/nav'
import { easeOutExpo } from '../utils/motion'

/**
 * 快速跳转面板（⌘K / Ctrl K）：
 * 模糊匹配导航与记忆命名空间，↑↓ 移动、Enter 打开、Esc 关闭。
 */
export default function QuickJump({
  groups,
  onClose,
  onNavigate,
}: {
  groups: NavGroup[]
  onClose: () => void
  onNavigate: (path: string) => void
}) {
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const hits = useMemo(() => searchNav(groups, query), [groups, query])

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    setCursor(0)
  }, [query])

  // 光标行始终保持在可视区内
  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>('[data-active="true"]')
    el?.scrollIntoView?.({ block: 'nearest' })
  }, [cursor])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key === 'ArrowDown' || (e.key === 'n' && e.ctrlKey)) {
        e.preventDefault()
        setCursor((c) => (hits.length ? (c + 1) % hits.length : 0))
        return
      }
      if (e.key === 'ArrowUp' || (e.key === 'p' && e.ctrlKey)) {
        e.preventDefault()
        setCursor((c) => (hits.length ? (c - 1 + hits.length) % hits.length : 0))
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        const hit = hits[cursor]
        if (hit) {
          onNavigate(hit.leaf.path)
          onClose()
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [hits, cursor, onClose, onNavigate])

  const open = (path: string) => {
    onNavigate(path)
    onClose()
  }

  return (
    <div className="jump-backdrop" onClick={onClose}>
      <motion.div
        className="jump-panel"
        role="dialog"
        aria-modal="true"
        aria-label="快速跳转"
        onClick={(e) => e.stopPropagation()}
        initial={{ opacity: 0, y: -12, scale: 0.97 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: -8, scale: 0.98 }}
        transition={{ duration: 0.22, ease: easeOutExpo }}
      >
        <div className="jump-input-row">
          <input
            ref={inputRef}
            className="jump-input"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="跳转到页面、命名空间…"
            aria-label="搜索导航"
            autoComplete="off"
            spellCheck={false}
          />
          <span className="jump-esc mono" aria-hidden="true">
            ESC
          </span>
        </div>

        <div className="jump-list" ref={listRef}>
          {hits.length === 0 && <div className="jump-empty">没有匹配「{query}」的入口</div>}
          {hits.map((hit, i) => (
            <button
              key={`${hit.groupCode}-${hit.leaf.path}`}
              type="button"
              className={`jump-row${i === cursor ? ' is-cursor' : ''}`}
              data-active={i === cursor}
              onMouseEnter={() => setCursor(i)}
              onClick={() => open(hit.leaf.path)}
            >
              <span className="jump-row-glyph" aria-hidden="true">
                {hit.leaf.icon}
              </span>
              <span className="jump-row-label">{hit.leaf.label}</span>
              <span className="jump-row-group mono">{hit.group}</span>
              <RightOutlined className="jump-row-go" aria-hidden="true" />
            </button>
          ))}
        </div>

        <div className="jump-foot mono">
          <span>↑↓ 选择</span>
          <span>Enter 打开</span>
          <span>Esc 关闭</span>
        </div>
      </motion.div>
    </div>
  )
}

/** 面板包装：负责出场动画与命令面板的显隐。 */
export function QuickJumpHost({
  open,
  groups,
  onClose,
  onNavigate,
}: {
  open: boolean
  groups: NavGroup[]
  onClose: () => void
  onNavigate: (path: string) => void
}) {
  return (
    <AnimatePresence>{open && <QuickJump groups={groups} onClose={onClose} onNavigate={onNavigate} />}</AnimatePresence>
  )
}