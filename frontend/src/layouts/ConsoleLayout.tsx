import { Suspense, useState } from 'react'
import type { CSSProperties } from 'react'
import { Button, Drawer, Layout, Menu, Spin, Tooltip } from 'antd'
import {
  DashboardOutlined,
  ClusterOutlined,
  ThunderboltOutlined,
  FileTextOutlined,
  ApiOutlined,
  AppstoreOutlined,
  KeyOutlined,
  MenuOutlined,
  MoonOutlined,
  SunOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import { useLocation, useNavigate, useOutlet } from 'react-router'
import { AnimatePresence, motion } from 'motion/react'
import { useWsStatus } from '../api/ws'
import { useIsMobile } from '../utils/useIsMobile'
import { toggleThemeMode, useThemeMode } from '../theme'
import { easeOutExpo } from '../utils/motion'

const { Sider, Header, Content } = Layout

const MENU = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/channels', icon: <ClusterOutlined />, label: '渠道管理' },
  { key: '/models', icon: <ThunderboltOutlined />, label: '模型用量' },
  { key: '/custom-models', icon: <AppstoreOutlined />, label: '自定义模型' },
  { key: '/keys', icon: <KeyOutlined />, label: 'API Keys' },
  { key: '/logs', icon: <FileTextOutlined />, label: '请求日志' },
  { key: '/settings', icon: <SettingOutlined />, label: '配置管理' },
]

/** 品牌字标（桌面侧栏 / 移动端顶栏与抽屉共用）。 */
function Brand({ collapsed, onClick }: { collapsed?: boolean; onClick?: () => void }) {
  return (
    <div
      className="brand"
      style={{
        padding: collapsed ? '20px 0 16px' : '20px 0 16px 20px',
        textAlign: collapsed ? 'center' : 'left',
        fontSize: 16,
        cursor: 'pointer',
        whiteSpace: 'nowrap',
        overflow: 'hidden',
      }}
      onClick={onClick}
    >
      {collapsed ? <ApiOutlined style={{ color: 'var(--accent)' }} /> : (
        <>
          <span className="accent">◆ </span>
          <span style={{ color: 'var(--text-primary)' }}>XToken</span>
          <span style={{ color: 'var(--text-faint)' }}>Hub</span>
        </>
      )}
    </div>
  )
}

/** WS 状态指示。 */
function LiveBadge() {
  const status = useWsStatus()
  const [cls, label] =
    status === 'open'
      ? ['live-dot', '实时推送已连接']
      : status === 'connecting'
        ? ['live-dot warn', '正在连接实时推送…']
        : ['live-dot off', '实时推送已断开，自动重连中']
  return (
    <Tooltip title={label}>
      <span className="ws-pill" style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
        <span className={cls} />
        <span className="mono" style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
          {status === 'open' ? 'LIVE' : status === 'connecting' ? 'SYNC' : 'OFF'}
        </span>
      </span>
    </Tooltip>
  )
}

/** 深浅主题切换按钮。 */
function ThemeToggle() {
  const mode = useThemeMode()
  return (
    <Button
      type="text"
      icon={mode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
      onClick={toggleThemeMode}
      aria-label={mode === 'dark' ? '切换到浅色主题' : '切换到深色主题'}
      title={mode === 'dark' ? '切换到浅色主题' : '切换到深色主题'}
      style={{ color: 'var(--text-secondary)', fontSize: 15 }}
    />
  )
}

const HEADER_STYLE: CSSProperties = {
  background: 'var(--header-bg)',
  borderBottom: '1px solid var(--border-faint)',
  display: 'flex',
  alignItems: 'center',
  paddingInline: 16,
  height: 52,
  lineHeight: '52px',
  position: 'sticky',
  top: 0,
  zIndex: 10,
  backdropFilter: 'blur(8px)',
}

/** 背景极光：缓慢漂移的柔和色块，营造深邃空间感（pointer-events:none，不干扰交互）。 */
function AuroraBg() {
  return (
    <div className="bg-aurora" aria-hidden="true">
      <div className="aurora-blob blob-1" />
      <div className="aurora-blob blob-2" />
    </div>
  )
}

/** 页面切换：路由变化时旧页淡出、新页上浮淡入（keyed by pathname）。
 *  Suspense 放在内容区内部：懒加载页面下载期间侧栏/顶栏保持可见，只让内容区显示兜底。 */
function PageTransition() {
  const location = useLocation()
  const outlet = useOutlet()
  return (
    <AnimatePresence mode="wait">
      <motion.div
        key={location.pathname}
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: -8 }}
        transition={{ duration: 0.3, ease: easeOutExpo }}
      >
        <Suspense
          fallback={
            <div style={{ display: 'flex', justifyContent: 'center', padding: '80px 0' }}>
              <Spin />
            </div>
          }
        >
          {outlet}
        </Suspense>
      </motion.div>
    </AnimatePresence>
  )
}

/** 控制台布局：桌面为 侧栏+顶栏+内容区；移动端为 顶栏(汉堡)+内容区+抽屉导航。 */
export default function ConsoleLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(false)
  const [navOpen, setNavOpen] = useState(false)
  const isMobile = useIsMobile()
  const mode = useThemeMode()
  const antdMenuTheme = mode === 'dark' ? 'dark' : 'light'

  const selected =
    MENU.find((m) => m.key !== '/' && location.pathname.startsWith(m.key))?.key ?? '/'

  const go = (key: string) => {
    navigate(key)
    setNavOpen(false)
  }

  if (isMobile) {
    return (
      <div style={{ minHeight: '100dvh', position: 'relative', zIndex: 1 }}>
        <AuroraBg />
        <Header style={{ ...HEADER_STYLE, justifyContent: 'space-between', paddingInline: 8 }}>
          <div style={{ display: 'inline-flex', alignItems: 'center', gap: 2, minWidth: 0 }}>
            <Button
              type="text"
              icon={<MenuOutlined />}
              onClick={() => setNavOpen(true)}
              aria-label="打开导航菜单"
              style={{ color: 'var(--text-secondary)', fontSize: 16 }}
            />
            <div style={{ overflow: 'hidden' }}>
              <Brand onClick={() => go('/')} />
            </div>
          </div>
          <div style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
            <ThemeToggle />
            <LiveBadge />
          </div>
        </Header>
        <Content style={{ padding: '12px 12px calc(24px + env(safe-area-inset-bottom))' }}>
          <PageTransition />
        </Content>
        <Drawer
          placement="left"
          width={248}
          open={navOpen}
          onClose={() => setNavOpen(false)}
          closable={false}
          styles={{ body: { background: 'var(--bg-base)', padding: 0, borderRight: '1px solid var(--border-faint)' } }}
        >
          <Brand onClick={() => go('/')} />
          <Menu
            theme={antdMenuTheme}
            mode="inline"
            selectedKeys={[selected]}
            items={MENU}
            onClick={({ key }) => go(key)}
            style={{ background: 'transparent', borderInlineEnd: 'none' }}
          />
        </Drawer>
      </div>
    )
  }

  return (
    <Layout style={{ height: '100vh', background: 'transparent', position: 'relative', zIndex: 1, overflow: 'hidden' }}>
      <AuroraBg />
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        width={200}
        className="sider-panel"
        theme={antdMenuTheme}
        style={{ background: 'transparent', borderRight: '1px solid var(--border-faint)', height: '100vh', overflowY: 'auto' }}
      >
        <Brand
          collapsed={collapsed}
          onClick={() => navigate('/')}
        />
        <Menu
          theme={antdMenuTheme}
          mode="inline"
          selectedKeys={[selected]}
          items={MENU}
          onClick={({ key }) => navigate(key)}
          style={{ background: 'transparent', borderInlineEnd: 'none' }}
        />
      </Sider>

      <Layout style={{ background: 'transparent', height: '100vh', overflowY: 'auto' }}>
        <Header style={{ ...HEADER_STYLE, justifyContent: 'flex-end', paddingInline: 24 }}>
          <div style={{ display: 'inline-flex', alignItems: 'center', gap: 12 }}>
            <ThemeToggle />
            <LiveBadge />
          </div>
        </Header>
        <Content style={{ padding: 24 }}>
          <PageTransition />
        </Content>
      </Layout>
    </Layout>
  )
}
