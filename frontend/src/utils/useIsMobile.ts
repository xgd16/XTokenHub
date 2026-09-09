import { useSyncExternalStore } from 'react'

/** 与 antd Grid 的 md 断点（min-width: 768px）取反，等价于「视口 < 768px」。 */
const QUERY = '(max-width: 767.98px)'

/** 兼容旧版 Safari 的 addListener/removeListener。 */
function subscribe(onChange: () => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => {}
  const mql = window.matchMedia(QUERY)
  if (mql.addEventListener) {
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }
  mql.addListener(onChange)
  return () => mql.removeListener(onChange)
}

/** 首帧即返回真实值（useSyncExternalStore 同步读取），避免移动端先按桌面渲染再纠正。 */
function getSnapshot(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia(QUERY).matches
}

/** 是否移动端视口（< 768px）。 */
export function useIsMobile(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, () => false)
}
