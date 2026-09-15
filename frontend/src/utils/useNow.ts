import { useEffect, useState } from 'react'

/** 周期性跳动的当前时间戳（毫秒），驱动随真实时间变化的 UI（如峰谷时段标记）。 */
export function useNow(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(id)
  }, [intervalMs])
  return now
}
