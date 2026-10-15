/**
 * jsdom 没有 canvas 实现（`getContext('2d')` 返回 null），而 cytoscape 的渲染器
 * 启动时就会 `getContext('2d')` 并直接抛 "Could not create canvas of type 2d"。
 *
 * 这里给出一个只够 cytoscape 跑通布局/命中测试的 2D 上下文桩：
 * - 未知方法通过 Proxy 一律返回 noop，避免 cytoscape 换版本就炸；
 * - 只有 `measureText` / 渐变 / `getImageData` 返回可用的结构，因为 cytoscape 会读它们的字段。
 */

const NOOP = () => undefined

type Ctx = Record<string, unknown> & {
  measureText: (t: string) => { width: number }
  createLinearGradient: () => { addColorStop: (offset: number, color: string) => void }
  createRadialGradient: () => { addColorStop: (offset: number, color: string) => void }
  createPattern: () => unknown
  getImageData: (x: number, y: number, w: number, h: number) => { data: Uint8ClampedArray; width: number; height: number }
  createImageData: (w: number, h: number) => { data: Uint8ClampedArray; width: number; height: number }
  getLineDash: () => number[]
  canvas: HTMLCanvasElement
}

const gradient = () => ({ addColorStop: NOOP })
const imageData = (w: number, h: number) => ({ data: new Uint8ClampedArray(Math.max(0, w * h * 4)), width: w, height: h })

/** 单字宽度：够 cytoscape 做标签宽度估算与换行判断。 */
function measure(text: string): { width: number } {
  return { width: String(text).length * 6.2 }
}

function makeContext(canvas: HTMLCanvasElement): Ctx {
  const base: Record<string, unknown> = {
    canvas,
    // cytoscape 会读写的状态字段
    fillStyle: '#000',
    strokeStyle: '#000',
    lineWidth: 1,
    globalAlpha: 1,
    globalCompositeOperation: 'source-over',
    font: '12px sans-serif',
    textAlign: 'center',
    textBaseline: 'middle',
    lineCap: 'butt',
    lineJoin: 'miter',
    miterLimit: 10,
    shadowBlur: 0,
    shadowColor: 'transparent',
    lineDashOffset: 0,
    // 结构性方法
    measureText: measure,
    createLinearGradient: gradient,
    createRadialGradient: gradient,
    createPattern: () => null,
    getImageData: (_x: number, _y: number, w: number, h: number) => imageData(w, h),
    createImageData: (w: number, h: number) => imageData(w, h),
    getLineDash: () => [],
    isPointInPath: () => false,
    getTransform: () => ({ a: 1, b: 0, c: 0, d: 1, e: 0, f: 0 }),
  }
  // 其余（save/restore/arc/fill/stroke/...）一律 noop
  return new Proxy(base, {
    get: (t, k: string) => (k in t ? t[k] : NOOP),
    set: (t, k: string, v) => {
      t[k] = v
      return true
    },
  }) as Ctx
}

/** 给 jsdom 打上 canvas 补丁（幂等）。 */
export function installCanvasStub(): void {
  const proto = globalThis.HTMLCanvasElement?.prototype
  if (!proto || (proto as { __xtkStub?: boolean }).__xtkStub) return
  ;(proto as { __xtkStub?: boolean }).__xtkStub = true
  proto.getContext = function getContext(this: HTMLCanvasElement, kind: string) {
    return kind === '2d' ? makeContext(this) : null
  } as HTMLCanvasElement['getContext']
  // jsdom 缺 toDataURL，cytoscape 导出图片时才用到
  proto.toDataURL = () => 'data:image/png;base64,'
  // getBoundingClientRect 恒为 0，cytoscape 的 fit/zoom 需要真实尺寸
  proto.getBoundingClientRect = function getBoundingClientRect(this: HTMLCanvasElement) {
    const w = Number.parseFloat(this.style.width) || this.width || 300
    const h = Number.parseFloat(this.style.height) || this.height || 150
    return { x: 0, y: 0, left: 0, top: 0, right: w, bottom: h, width: w, height: h, toJSON: () => ({}) } as DOMRect
  }
}
