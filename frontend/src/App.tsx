import { lazy, useEffect, useMemo } from 'react'
import { App as AntdApp, ConfigProvider, theme } from 'antd'
import type { ThemeConfig } from 'antd'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { MotionConfig } from 'motion/react'
import zhCN from 'antd/locale/zh_CN'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import ConsoleLayout from './layouts/ConsoleLayout'
import { initWS } from './api/ws'
import { useThemeMode, type ThemeMode } from './theme'
import './styles/global.css'

// 路由级懒加载：首屏只加载仪表盘，其余页面按访问拆分，避免单包 1.3MB 全量下载。
const Dashboard = lazy(() => import('./pages/Dashboard'))
const Channels = lazy(() => import('./pages/Channels'))
const Models = lazy(() => import('./pages/Models'))
const CustomModels = lazy(() => import('./pages/CustomModels'))
const Keys = lazy(() => import('./pages/Keys'))
const RequestLogs = lazy(() => import('./pages/RequestLogs'))
const Settings = lazy(() => import('./pages/Settings'))

dayjs.locale('zh-cn')

const FONT_FAMILY =
  "'IBM Plex Sans', 'PingFang SC', 'Microsoft YaHei', system-ui, sans-serif"

/** 深浅两套 antd token，与 global.css 的 CSS 变量保持同源色值。 */
function antdTheme(mode: ThemeMode): ThemeConfig {
  const dark = mode === 'dark'
  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: dark ? '#2fe0a4' : '#0c9d74',
      colorInfo: dark ? '#56c8ff' : '#0d84c9',
      colorBgBase: dark ? '#0b1017' : '#f3f5f7',
      colorBgContainer: dark ? '#10161f' : '#ffffff',
      colorBgElevated: dark ? '#141c27' : '#ffffff',
      colorBorder: dark ? 'rgba(94, 128, 148, 0.22)' : 'rgba(45, 74, 96, 0.22)',
      colorBorderSecondary: dark ? 'rgba(94, 128, 148, 0.14)' : 'rgba(45, 74, 96, 0.10)',
      colorText: dark ? '#e6edf3' : '#1a2530',
      colorTextSecondary: dark ? '#8b98a9' : '#5a6a7a',
      borderRadius: 6,
      fontFamily: FONT_FAMILY,
    },
    components: {
      Table: {
        headerBg: 'transparent',
        rowHoverBg: dark ? 'rgba(47, 224, 164, 0.05)' : 'rgba(12, 157, 116, 0.06)',
        borderColor: dark ? 'rgba(94, 128, 148, 0.14)' : 'rgba(45, 74, 96, 0.10)',
      },
      Menu: {
        itemBg: 'transparent',
        itemSelectedBg: dark ? 'rgba(47, 224, 164, 0.12)' : 'rgba(12, 157, 116, 0.10)',
        itemSelectedColor: dark ? '#2fe0a4' : '#0c9d74',
      },
      Card: { colorBgContainer: 'transparent' },
    },
  }
}

/** 应用根：主题（深/浅）+ 全局 WS 初始化 + 路由。 */
export default function App() {
  const mode = useThemeMode()
  const themeConfig = useMemo(() => antdTheme(mode), [mode])

  useEffect(() => {
    initWS()
  }, [])

  return (
    <ConfigProvider locale={zhCN} theme={themeConfig}>
      <AntdApp>
        {/* reducedMotion="user"：系统开启「减弱动态效果」时自动跳过 transform/布局动画 */}
        <MotionConfig reducedMotion="user">
          <BrowserRouter>
            <Routes>
              <Route element={<ConsoleLayout />}>
                <Route path="/" element={<Dashboard />} />
                <Route path="/channels" element={<Channels />} />
                <Route path="/models" element={<Models />} />
                <Route path="/custom-models" element={<CustomModels />} />
                <Route path="/keys" element={<Keys />} />
                <Route path="/logs" element={<RequestLogs />} />
                <Route path="/settings" element={<Settings />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Route>
            </Routes>
          </BrowserRouter>
        </MotionConfig>
      </AntdApp>
    </ConfigProvider>
  )
}
