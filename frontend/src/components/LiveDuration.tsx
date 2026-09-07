import { useEffect, useReducer } from 'react'
import { duration } from '../utils/format'

/** 进行中请求的实时耗时：每 500ms 刷新一次自 from 起的经过时间。 */
export default function LiveDuration({ from }: { from: string }) {
  const [, force] = useReducer((x: number) => x + 1, 0)
  useEffect(() => {
    const t = setInterval(force, 500)
    return () => clearInterval(t)
  }, [])
  const ms = Date.now() - new Date(from).getTime()
  return (
    <span className="mono" style={{ color: 'var(--accent)' }}>
      {duration(Number.isFinite(ms) && ms > 0 ? ms : 0)}
    </span>
  )
}
