# XTokenHubMenuBar

XTokenHub 的 macOS 菜单栏伴侣应用 —— 类 iStat Menus 的 token 用量监控,基于 SwiftUI + Liquid Glass(液态玻璃,macOS 26+)。

纯前端实现:**零后端改动**,仅消费 XTokenHub 现有的管理 API(`/api/v1/*`)与 WebSocket 长连接(`/api/v1/ws`)。

## 功能

- **菜单栏实时指标自由组合**:设置中按需勾选 图标 / 今日请求数 / 今日 Token / 缓存命中率 / 今日花费 / 实时速度,如 `482 · 3214.45万 · 96.0% · $1.234 · 12/s`(默认 数量+速度)
- **下拉面板**(内容对齐 Web 仪表盘;**系统 Liquid Glass 为主材质**,分区为独立玻璃模块:
  - 玻璃形状「少而大」:九宫格等小区块合并成一块玻璃 + 内部发丝线分格,不做「每小块各自玻璃 + 密排」
    —— 小玻璃的镜面硬边会在 10pt 窄缝里互相挤压成脏灰边(液态玻璃只适合大面、且要有间隔)
  - 分区间距 16pt > `GlassEffectContainer` spacing 8pt,相邻玻璃不半融合,间隔里透出下层
  - 面板内部不使用投影、不加不透明底色:玻璃的折射与高光自身就是层次
  - 玻璃档位取 `.clear`(文档中更透的一档;`.regular` 是可读性优先档,会主动加底衬、天生偏实)。
    系统设置 → 外观 → Liquid Glass 的「清透 / 着色」会整体调节系统玻璃透明度,同样作用于这里
  - 已知上限:面板自身已是一层系统玻璃,内容再叠一层玻璃 —— 两层各自提亮一次,这是「看着像毛玻璃」的主因,
    也是 `MenuBarExtra` 下透明度的天花板。要更透只能去掉其中一层(内容直接坐在面板玻璃上,不再自建玻璃面)
  - 连接状态 + 实时输出速度 X/s + 活跃流数(WS `stats.throughput`,2Hz)
  - 九宫格统计:请求/错误/Token·今日、缓存命中、平均耗时、透传占比、累计 Token、峰值 Token、连续天数。同一张面内以发丝线分格(而非九个独立玻璃磁贴),大数值的 万/亿 数量级降为次级字号(悬停查看口径)
  - 花费卡:今日花费(+预计今日)、本月累计(+月度预算与超支时点)、本月预计(近 7 日均速/本周期线性 + 置信度),口径同 Web 仪表盘
  - 渠道与余额卡:列出接入渠道(启用/停用状态色点、接口风格)与上游账户余额;DeepSeek 按官方接口查询,余额以**上游原币种**展示(如 ¥),其余渠道显示 `—`
  - Token 趋势图 + 范围切换:实时(分钟桶)/ 24h / 7天 / 30天(Swift Charts)
  - 模型 TOP · 今日(条形排行)
  - 调用方 TOP · 30 天(`stats/by-key`,同 Web 口径)
  - 实时请求流:`request.started` 插入"生成中"行,`request.completed` 按 `req_id` 原位替换;三行富行常驻展示协议/转发模式/模型/状态码/时间、渠道·调用方·客户端(UA 短名)、入出 Token·缓存命中·输出速度·耗时·花费,错误行红色高亮,悬停查看会话/请求头/IP/计价口径等完整明细
  - 底部操作:打开 Web 控制台 / 设置 / 退出
- **设置**:Hub 服务地址(默认 `http://127.0.0.1:9192`,支持远程部署)、菜单栏显示指标(含今日花费)、登录自启(SMAppService)
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
make package              # 制作 DMG 安装包(Release 构建,输出 XTokenHubMenuBar-1.2.0.dmg)
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
| 接入渠道 | `GET /api/v1/channels?page=1&per_page=200`(不消费 `api_key`) |
| 渠道余额 | `GET /api/v1/channels/balances`(按 BaseURL 推断厂家,服务端 5 分钟 TTL) |
| 花费预测 | `GET /api/v1/stats/cost/forecast?period=today\|month` |
| 计费设置 | `GET /api/v1/settings/billing`(展示币种/汇率/月预算) |
| 实时推送 | `WS /api/v1/ws`(`request.started/completed`、`stats.updated`、`stats.throughput`、`channel.balance_updated`、`channel.status_changed/probe_result`) |

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
└── Utilities/                  # TokenFormatter / MoneyFormatter / HubDate / HubURL
Tests/                          # Swift Testing 单元测试(模型解码/格式化/URL 规范化)
Tools/make_icon.swift           # 重新生成占位 AppIcon
```

## 技术要点

- **数字单位与 Web 前端完全一致**:token 数与速度均采用 `compactCN` 规则(≥1亿 → `X亿`、≥1万 → `X万`,≤2 位小数去尾零),只显示数字与单位、不带 "tok" 后缀;耗时用 `duration()` 规则 —— 见 `frontend/src/utils/format.ts`
- **Liquid Glass**:`GlassEffectContainer` 聚合相邻卡片、`.glassEffect(.regular)` 卡片材质、`.glassEffect(.regular.interactive())` 磁贴、`.buttonStyle(.glass)` 操作按钮;深浅色自动适配
- **实时流配对逻辑**与前端 Dashboard 一致:`isPending = id == 0 && req_id > 0`,completed 事件按 `req_id` 查找原位替换
- Swift 6 严格并发:全部可变状态 `@MainActor` 收敛,`@Observable` 驱动 UI
- 零第三方依赖:Charts、URLSessionWebSocketTask 均为系统能力
