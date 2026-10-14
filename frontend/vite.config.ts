import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// 前端构建产物输出到 ../web/dist，供 go:embed 打包进二进制
// 开发代理默认指向本机后端（127.0.0.1:9192）；如需联调局域网设备，
// 用环境变量覆盖：VITE_API_TARGET=http://192.168.1.110:9192 make dev
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', '')
  const apiTarget = env.VITE_API_TARGET || 'http://127.0.0.1:9192'
  return {
    plugins: [react()],
    server: {
      port: 5173,
      proxy: {
        '/api': { target: apiTarget, changeOrigin: true, ws: true },
        '/v1': { target: apiTarget, changeOrigin: true },
      },
    },
    build: {
      outDir: '../web/dist',
      emptyOutDir: true,
      sourcemap: false,
    },
  }
})
