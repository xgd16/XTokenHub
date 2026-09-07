import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 前端构建产物输出到 ../web/dist，供 go:embed 打包进二进制
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:9192', changeOrigin: true, ws: true },
      '/v1': { target: 'http://127.0.0.1:9192', changeOrigin: true },
    },
  },
  build: {
    outDir: '../web/dist',
    emptyOutDir: true,
    sourcemap: false,
  },
})
