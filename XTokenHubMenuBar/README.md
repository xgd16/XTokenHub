# XTokenHubMenuBar

XTokenHub 的 macOS 菜单栏伴侣应用 —— 类 iStat Menus 的 token 用量监控,基于 SwiftUI + Liquid Glass(液态玻璃,macOS 26+)。

纯前端实现:**零后端改动**,仅消费 XTokenHub 现有的管理 API(`/api/v1/*`)与 WebSocket 长连接(`/api/v1/ws`)。

## 功能

- **菜单栏实时指标自由组合**:设置中按需勾选 图标 / 今日请求数 / 今日 Token / 缓存命中率 / 实时速度,如 `482 · 3214.45万 · 96.0% · 12/s`(默认 数量+速度)
- **下拉玻璃面板**(内容对齐 Web 仪表盘):
  - 连接状态 + 实时输出速度 X/s + 活跃流数(WS `stats.throughput`,2Hz)
  - 九宫格统计磁贴:请求/错误/Token·今日、缓存命中、平均耗时、透传占比、累计 Token、峰值 Token、连续天数(悬停查看口径)
  - Token 趋势图 + 范围切换:实时(分钟桶)/ 24h / 7天 / 30天(Swift Charts)
  - 模型 TOP · 今日(条形排行)
  - 调用方 TOP · 30 天(`stats/by-key`,同 Web 口径)
  - 实时请求流:`request.started` 插入"生成中"行,`request.completed` 按 `req_id` 原位替换;错误行红色高亮,悬停查看 UA/IP/错误详情
  - 底部操作:打开 Web 控制台 / 设置 / 退出
- **设置**:Hub 服务地址(默认 `http://127.0.0.1:9192`,支持远程部署)、菜单栏显示模式、登录自启(SMAppService)
- 连接保活:文本心跳 25s、65s 静默判死、指数退避自动重连(1s→30s),重连后自动全量补拉

## 环境要求

- macOS 26+(Liquid Glass 为 macOS 26 原生 API,无旧系统兼容分支)
- Xcode 26+
- [XcodeGen](https://github.com/yonaskolb/XcodeGen)(`brew install xcodegen`)

## 构建与运行

```bash
brew install xcodegen     # 首次
make run                  # 生成工程 → 编译 → 启动
make test                 # 单元测试
make gen                  # 仅重新生成 .xcodeproj(修改 project.yml 后)
make icon                 # 重新生成 AppIcon(1024 master + 多尺寸派生)
make package              # 制作 DMG 安装包(Release 构建,输出 XTokenHubMenuBar-1.0.0.dmg)
```

安装:打开 DMG,把 XTokenHubMenuBar.app 拖入 Applications 即可(应用为 ad-hoc 签名;拷入 /Applications 后才能正常注册「登录时自动启动」)。图标由 `Tools/make_icon.swift` 程序化绘制(蓝青渐变玻璃底 + 上升折线),改配色/形状后重跑 `make icon && make package`。

应用为纯菜单栏应用(`LSUIElement`),启动后出现在系统菜单栏右上角。点击图标展开面板;设置面板中可修改 Hub 地址,修改后自动重连。

> 「登录时自动启动」需要先将 `XTokenHubMenuBar.app` 拷入 `/Applications`(系统限制)。

## 数据来源(与 XTokenHub 对应)

| 用途 | 端点 |
|---|---|
| 今日汇总/缓存命中/耗时 | `GET /api/v1/stats/summary?hours=24&since=<当日零点>` |
| 迷你趋势图 | `GET /api/v1/stats/trend?hours=1&bucket=minute` |
| 模型 TOP | `GET /api/v1/stats/by-model?since=<当日零点>` |
| 累计 Token | `GET /api/v1/stats/lifetime` |
| 实时流首屏 | `GET /api/v1/logs?page=1&per_page=30` |
| 实时推送 | `WS /api/v1/ws`(`request.started/completed`、`stats.updated`、`stats.throughput`、`channel.*`) |

## 代码结构

```
App/
├── XTokenHubMenuBarApp.swift   # 入口:MenuBarExtra(.window) + Settings 场景
├── Core/
│   ├── Models/                 # 与 Go 端 json 标签对齐的 Codable + WS 事件解码
│   ├── Networking/
│   │   ├── APIClient.swift     # async/await REST 客户端(信封解包)
│   │   └── HubSocket.swift     # WebSocket:心跳/判死/退避重连
│   └── Store/
│       ├── HubStore.swift      # @Observable 主线程状态机(REST + WS 事件汇聚)
│       └── AppSettings.swift   # UserDefaults 持久化设置
├── Views/                      # 玻璃面板、菜单栏标签、设置页、组件
└── Utilities/                  # TokenFormatter / HubDate / HubURL
Tests/                          # Swift Testing 单元测试(模型解码/格式化/URL 规范化)
Tools/make_icon.swift           # 重新生成占位 AppIcon
```

## 技术要点

- **数字单位与 Web 前端完全一致**:token 数与速度均采用 `compactCN` 规则(≥1亿 → `X亿`、≥1万 → `X万`,≤2 位小数去尾零),只显示数字与单位、不带 "tok" 后缀;耗时用 `duration()` 规则 —— 见 `frontend/src/utils/format.ts`
- **Liquid Glass**:`GlassEffectContainer` 聚合相邻卡片、`.glassEffect(.regular)` 卡片材质、`.glassEffect(.regular.interactive())` 磁贴、`.buttonStyle(.glass)` 操作按钮;深浅色自动适配
- **实时流配对逻辑**与前端 Dashboard 一致:`isPending = id == 0 && req_id > 0`,completed 事件按 `req_id` 查找原位替换
- Swift 6 严格并发:全部可变状态 `@MainActor` 收敛,`@Observable` 驱动 UI
- 零第三方依赖:Charts、URLSessionWebSocketTask 均为系统能力
