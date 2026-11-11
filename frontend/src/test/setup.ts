// Vitest 全局 setup：补齐 jsdom 缺失的浏览器 API（canvas 2D 上下文、matchMedia）。
import { installCanvasStub } from './canvasStub'

if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
}

// cytoscape 渲染器启动即 getContext('2d')，jsdom 返回 null 会直接抛错。
installCanvasStub()

// ECharts 封装依赖 ResizeObserver；jsdom 没有实现，先给一个空桩。
// 需要真实尺寸的用例会在各自测试里 vi.stubGlobal 覆盖它。
if (typeof globalThis.ResizeObserver === 'undefined') {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
}
