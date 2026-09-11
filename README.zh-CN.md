<div align="center">

<img src="docs/logo.png" width="96" alt="XTokenHub Logo"/>

# XTokenHub

[English](README.md) | 简体中文

**AI API 聚合网关 · 统一管理你的所有模型 Token**

把各模型厂家的上游 API 汇聚成渠道，对外统一暴露三种标准协议端点，实时统计 token 用量与缓存命中率。

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-1.x-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)
![SQLite](https://img.shields.io/badge/SQLite-零%20CGO-003B57?logo=sqlite&logoColor=white)
![Test](https://img.shields.io/badge/覆盖率-87.6%25-brightgreen)

</div>

---

**核心原则：能用原生协议就原生透传，不做格式转换；转换只作为兜底路径。**

| 仪表盘 | 渠道管理 |
|---|---|
| ![仪表盘](docs/screenshot-dashboard.png) | ![渠道管理](docs/screenshot-channels.png) |
| **模型用量** | **API Keys** |
| ![模型用量](docs/screenshot-model-usage.png) | ![API Keys](docs/screenshot-apikeys.png) |
| **macOS 菜单栏伴侣应用** | |
| <img src="docs/screenshot-menubar.png" width="360" alt="macOS 菜单栏应用"/> | |

## ✨ 主要功能

- **🔑 Token 统一管理** —— 到处散落的厂家 API Key 收拢到一个面板：渠道化录入、一键探测可用性、启停切换、余额查询（如 DeepSeek），再也不用在配置文件里翻找密钥；
- **🗂 模型分组** —— 每条渠道挂载自己的模型清单（支持从上游一键拉取），多个上游聚合为统一的 `/v1/models` 视图；按 priority / weight 加权路由，同模型多渠道自动 failover；
- **📊 使用量快速了解** —— 仪表盘实时呈现请求数、token 用量、缓存命中率、平均耗时，按模型 / 渠道 / 调用方密钥多维聚合，配 GitHub 风格 Token 活动热力图与每日趋势图，WebSocket 实时推送；
- **🔄 协议转换** —— 对外同时暴露 OpenAI（`/v1/chat/completions`、`/v1/responses`）与 Anthropic（`/v1/messages`）三端点，入站与上游协议不一致时自动转换；一致则零转换原生透传，保住工具调用、多模态等完整能力；
- **💰 花费统计与预计** —— 每条请求按上游 usage 口径即时计费（区分 OpenAI / Anthropic 的缓存计费差异），仪表盘展示今日/本月已花费与**本月预计花费**，价格表可从 LiteLLM 公开数据一键同步（约 2900 个模型）并支持手工覆盖，支持 USD/CNY 双币展示；
- **🧾 网关密钥与按调用方统计** —— 签发 `sk-xt-*` 网关密钥分发给不同客户端，按密钥聚合请求数与 token，谁用得多一目了然；
- **📦 单二进制自托管** —— 前端经 go:embed 内嵌，`make build` 产出一个静态二进制（零 CGO），拷到任何 Linux/macOS 机器即可跑。

**技术栈**：后端 Go + Gin + GORM + SQLite（纯 Go 驱动 glebarez/sqlite）+ Viper + gorilla/websocket；前端 React 19 + TypeScript + Ant Design 6 + Vite；每个模块配单元测试，`go test ./... -race` 全绿，语句覆盖率 87.6%。

## 架构

```
cmd/server/          入口：配置 -> DB -> 事件总线 -> WS Hub -> 路由 -> 优雅退出
internal/
  config/            Viper 配置（默认值 <- yaml <- XT_HUB_ 环境变量）
  database/          GORM + SQLite 初始化与 AutoMigrate
  model/             Channel / RequestLog 与协议枚举
  repository/        数据访问接口 + GORM 实现（接口化，service 可 mock）
  service/           业务逻辑：渠道 CRUD、协议探测、日志、统计
  handler/
    admin/           管理 API（渠道/日志/统计/WS）
    gateway/         网关三端点（解析入站请求 -> 执行器）
  gateway/           编排：渠道选择（原生优先）+ 透传/转换 + 流式转发 + 统计落库
  provider/          上游对接：端点定位、原生协议探测器、协议转换器、usage 解析、token 估算
  eventbus/          内存发布订阅总线（业务与推送解耦）
  ws/                WebSocket Hub：连接管理、心跳保活、异步广播、慢消费者摘除
  middleware/        CORS / 访问日志 / Recovery
  router/            路由注册 + 前端 SPA 托管（/api、/v1 除外）
  pkg/               resp 统一响应 / errs 错误码 / pagination / logger
web/web.go           go:embed 前端产物（web/dist）
frontend/            React 后台源码（仪表盘 / 渠道管理 / 请求日志 / 模型用量）
internal/tests/      全部单元测试（按模块镜像组织，外部测试包只测导出 API）
```

## 网关工作流

1. **创建渠道**：录入厂家 BaseURL / API Key / 模型清单（可一键「从上游拉取」：调用上游模型列表接口取回真实模型勾选）。接口风格（OpenAI 兼容 Bearer / Anthropic 兼容 x-api-key）默认按 BaseURL 自动识别（域名或路径含 anthropic 即 Anthropic 风格），可手动覆盖；同一厂家的不同协议端点各建一条渠道——如 DeepSeek：OpenAI 面 `https://api.deepseek.com`、Anthropic 面 `https://api.deepseek.com/anthropic`（网关拼装为 `.../anthropic/v1/messages`）。BaseURL 支持三种挂载形态：裸域名（端点补 `/v1/`）、`/v1` 结尾（等价裸域名）、子路径挂载点（如 `/anthropic`，拼 `/v1/messages`）、版本化挂载点（如智谱 `https://open.bigmodel.cn/api/paas/v4`，直接拼 `.../v4/chat/completions`，不重复插版本段）。模型列表优先渠道自身 `.../models`，不可用（如 DeepSeek 子路径无列表接口）自动降级尝试主域名 `/v1/models`；
2. **原生协议探测**：对协议端点逐一发送最小请求（max_tokens=1），2xx 即标记为该渠道的原生协议（401/404/5xx 均不算，可手工修正 `native_protocols`）；裸域名 BaseURL 探测全部三协议（自动识别中转站的多协议支持），子路径 BaseURL（如 DeepSeek 的 `https://api.deepseek.com/anthropic`）视为协议挂载点、只探测该厂家协议家族；未显式指定探测模型时自动选取真实模型：上游自身模型列表 -> 渠道配置模型 -> 主域名兜底列表 -> 内置兜底，避免用不存在的模型误判；
3. **请求路由**：按模型筛选启用渠道 -> **优先原生渠道（零转换透传）** -> 无原生渠道才走协议转换 -> priority 升序 + weight 加权随机 -> 失败自动 failover（网络错误 / 401 / 403 / 408 / 429 / 5xx）；
4. **计量统计**：优先解析上游 usage（OpenAI 流式自动加 `stream_options.include_usage`；Anthropic 取 `message_start`/`message_delta` 事件）；上游未报告时用本地启发式估算兜底；缓存命中率由 `cached_tokens`（OpenAI）/ `cache_read_input_tokens`（Anthropic）计算；每笔请求落 `request_logs` 并经 WebSocket 实时推送前端。

协议转换矩阵（兜底路径，v1 聚焦文本对话 + 常用采样参数；工具调用/多模态仅在透传路径保证完整）：

| 入站 \ 上游 | openai_compatible | anthropic |
|---|---|---|
| chat_completions | 原生透传 | 转换 |
| responses | 转换 | 转换 |
| messages | 转换 | 原生透传 |

> 转换上游不支持 responses 协议（OpenAI Responses API 语义过重，原生渠道优先）。

## WebSocket 实时推送

- 端点：`GET /api/v1/ws`；消息信封 `{type, payload, ts}`
- 事件：`request.completed`（每笔网关请求）、`stats.updated`、`channel.probe_result`、`channel.status_changed`、`channel.balance_updated`
- 业务只向 eventbus 发布，WS Hub 订阅广播——业务与推送完全解耦；服务端 ping/pong 保活、慢消费者自动摘除
- 前端 `src/api/ws.ts`：指数退避自动重连（1s 起、30s 封顶）、按类型订阅、连接状态驱动顶栏呼吸灯

## 仪表盘

- **当天口径**：顶部统计卡以「今天」（浏览器本地零点起，`since` 参数）统计请求/token/缓存命中率，不再是滚动 24 小时；
- **全历史累计卡**：累计 Token、峰值 Token（单日最高）、最长聊天时长（单次成功请求）、当前/最长连续使用天数（GitHub 连击口径，按日聚合推导）；
- **Token 活动热力图**：GitHub 风格日历（近 26 周，按本地日界聚合），支持 每日 / 每周 / 累计 三种着色模式，与「调用方 TOP · 30 天」（按网关密钥聚合请求数与 token）并排一行；
- **每日 Token 趋势图**：近 7 日 / 近 30 日切换，多模型平滑曲线（按日 × 模型聚合，前 6 名 + 其他）；
- **模型用量环图**：与趋势图同数据源、同色板，环心为区间总 token，图例含占比与用量；
- **图表交互**：趋势图悬浮出十字准线与当日各模型明细，环图悬浮加粗扇区并在中心切换占比、图例同步高亮，热力图悬浮显示当日 token；
- 实时请求流：按调用方会话（入站 `X-Session-Id` 头，落库为 `session_id`）自动合并为会话行，汇总请求数 / token / 缓存命中 / 耗时等综合值，点击行首箭头展开该会话的请求级明细；未携带会话头的调用方保持单行展示，每行「查看头」可查看该请求的完整入站请求头（`request_headers`）。

## 花费统计与预计

- **请求级费用**：每条请求在网关收到上游响应后即时计价并随日志落库（`cost_usd`），请求日志与仪表盘均可见；历史记录不会因价格调整而改变，需要时可在设置页「重算历史费用」；
- **花费卡片**：仪表盘展示今日花费、本月累计花费、本月预计花费，模型 TOP 与调用方 TOP 同时附带花费；
- **期末预测**：有 ≥ 3 个完整日数据时按近 7 日日均速率外推剩余时段，否则按本周期已花速率线性外推；周期刚开始（不足 10 分钟）或尚无花费时不给预测并说明原因，避免用极少样本给出误导性数字；配置月度预算后会额外推算「预计何时触及预算」；
- **价格表**：设置页可按模型名搜索、手工增删改单价，并可从 LiteLLM 公开价格表一键同步（约 2900 个模型，USD / 单 token，含缓存读写与长上下文分档）；手工配置的行标记为「手工」，后续同步不会覆盖；
- **未定价提示**：有用量但价格表未覆盖的模型会单独列出（费用按 0 计），补上单价后点「重算历史费用」即可补齐历史花费；
- **多币种**：价格行可标为人民币（`CNY`）或美元（`USD`），人民币价按设置页汇率折算成美元记账，`cost_usd` 始终是美元单一口径；展示币种可切人民币；
- **错峰（时段）价**：价格行可配置高峰时段，空闲时段按对应的 off-peak 费率计价；国内模型（如 DeepSeek）常见的「空闲时段半价」因此能如实入账。

### 计价口径

输入侧 token 按上游协议口径拆分，两种口径**不可混用**，否则会重复计费或漏计：

- **OpenAI 系**（`chat_completions` / `responses`）：`prompt_tokens` 已包含缓存命中，未命中部分 = `prompt − cached`，且不单独计缓存写；
- **Anthropic 系**（`messages`）：`input_tokens` 不含缓存读写，缓存读与实际写入各自单独计费。

因此费用必须在**网关写入时**计算并落库——落库字段只有入站协议，协议转换（如 chat 入站转 messages 上游）后无法再还原上游口径；日志里的 `usage_style` 记录了当时所用的口径，`price_period` 记录了命中的计价时段（`peak` / `off_peak`），均供审计与重算使用。

费率换算：公开价格表以「每百万 token」报价，本地按「单 token」存储，设置页表单同样按百万 token 录入。国内的「缓存命中」价即本项目的**缓存读**价（如 DeepSeek 空闲 0.02、高峰 0.04 元/百万）；**缓存读留空（0）时会回退为输入价**（与公开价格表口径一致），在缓存命中率高的场景会显著**高估**费用（命中 token 会被按贵得多的输入价计费），设置页编辑弹窗对此有明确提示。

### 币种与错峰价

价格表每行带一个币种：`USD`（同步来源恒为此值）或 `CNY`。人民币行按「计费与展示」里的 USD→CNY 汇率折算成美元后计入 `cost_usd`，因此所有聚合、预测与排行榜仍是单一美元口径。**汇率未配置（为 0）时人民币行的费用记为 0**（按未定价处理，不产出量纲错误的数字）；设置页会给出提示，填好汇率后点「重算历史费用」即可补全。

错峰价用**高峰时段**表达（空闲时段即其余时间），按模型配置：

```
<星期>;<时段>[,<时段>...]
```

- 星期：`1`=周一 … `7`=周日，支持区间与列表，如 `1-5`、`1,3,5`、`6-7`；
- 时段：`HH:MM-HH:MM`，多个用逗号分隔；结束须晚于开始，**不支持跨零点**（空闲时段用「列出高峰窗口」表达，无需环绕）；
- 判定时区固定为**北京时间（UTC+8）**，不随服务器时区变化。

DeepSeek 官方规则（工作日 9:00–12:00、14:00–18:00 为高峰）即：

```
1-5;09:00-12:00,14:00-18:00
```

设置页编辑价格时可点「套用 DeepSeek 模板」一键填入。空闲时段的 off-peak 费率留 0 表示回退高峰费率（时段仍会记录）。

已知限制：长上下文只支持**单档**（填写阈值后，prompt 超过阈值时整单改用「超阈值」费率；上游多档阶梯价按第一档近似），且空闲费率只作用于四个基础费率、不参与超阈值分档；公开价格表只有美元价与固定价，**不含时段价**，因此时段价与人民币价只能来自手工配置的行；上游未报告 usage、由本地估算 token 的请求，其费用同样是估算值；价格表未覆盖的模型费用记 0。

## API

管理端（`/api/v1`，暂未启用登录认证）：

```
GET    /api/v1/ws                     WebSocket 实时推送
GET    /api/v1/channels               渠道列表（分页）
POST   /api/v1/channels               创建渠道
POST   /api/v1/channels/lookup-models 拉取上游模型列表 {provider, base_url, api_key}
GET    /api/v1/channels/balances      渠道余额批量查询（按 BaseURL host 识别厂家，
                                      当前支持 DeepSeek，5 分钟 TTL 缓存 + WS 推送）
GET    /api/v1/channels/:id
PUT    /api/v1/channels/:id
DELETE /api/v1/channels/:id
POST   /api/v1/channels/:id/probe     触发原生协议探测 {model?}（缺省自动从上游模型列表选取）
GET    /api/v1/keys                   网关密钥列表（分页）
POST   /api/v1/keys                   创建密钥（服务端生成 sk-xt-*，响应含原文）
PUT    /api/v1/keys/:id               改名/启停/备注（key 本体不可改）
DELETE /api/v1/keys/:id
GET    /api/v1/logs                   请求日志（protocol/forward_mode/channel_id/key_id/model/stream/hours/error_only 筛选）
GET    /api/v1/logs/cleanup           日志清理状态（enabled/保留天数/上次运行信息）
POST   /api/v1/logs/cleanup           手动触发一次清理（返回删除行数/耗时/cutoff）
GET    /api/v1/stats/summary?hours=24 汇总（请求数/token/缓存命中率/平均耗时/透传占比；
                                      可传 since=<unix秒> 显式指定窗口起点，前端传本地零点即「当天」口径）
GET    /api/v1/stats/trend?days=7     按日趋势（days 最长 366，供热力图/长区间）
GET    /api/v1/stats/by-model?hours=24
GET    /api/v1/stats/by-channel?hours=24
GET    /api/v1/stats/by-key?hours=24  按调用方密钥聚合（请求数/token/缓存命中率）
GET    /api/v1/stats/lifetime         全历史累计（总 token/峰值日/最长单次耗时/连续使用天数）
GET    /api/v1/stats/trend-by-model?days=7 按日 × 模型 token 用量（多模型趋势线）
GET    /api/v1/stats/live-sessions?limit=20 最近活跃会话聚合（含花费口径的 token 汇总）
GET    /api/v1/stats/cost/forecast?period=today|month 花费预测（已花费/预测值/基准/置信度；
                                      设了月度预算时附 projected_exceeded_date）
GET    /api/v1/stats/cost/unpriced?hours=720 有用量但价格表未覆盖的模型
POST   /api/v1/stats/cost/recompute   重算历史费用 {from?,to?,only_missing?}
GET    /api/v1/settings/prices        价格表（分页 + q 模糊搜索 + used_only 过滤；
                                      附同步状态/未定价模型/计费设置）
POST   /api/v1/settings/prices        新增手工价格（费率单位 USD / 单 token）
POST   /api/v1/settings/prices/sync   立即从公开价格表同步（手工配置行保留）
PUT    /api/v1/settings/prices/:id    编辑价格（改后标记为手工配置，同步不再覆盖）
DELETE /api/v1/settings/prices/:id
GET    /api/v1/settings/billing       计费展示设置（币种/汇率/月度预算）
PUT    /api/v1/settings/billing
GET    /healthz
```

网关端点（OpenAI / Anthropic 客户端可直连，默认强制 API Key）：

```
GET  /v1/models            聚合各启用渠道的模型清单（OpenAI 规范格式，
                           附 Anthropic 的 type/display_name 字段，开发工具可直接识别）
POST /v1/chat/completions
POST /v1/responses
POST /v1/messages
```

### 网关鉴权与按调用方统计

- 「API Keys」页创建密钥（`sk-xt-` + 32 位随机十六进制），创建后自动复制到剪贴板；支持启停与删除（历史日志保留 key 名快照）
- 调用方携带方式：`Authorization: Bearer sk-xt-...`（OpenAI 兼容）或 `x-api-key: sk-xt-...`（Anthropic 兼容）；缺失/错误/停用返回 401（按入站协议的错误格式）
- 每笔请求日志记录 `key_id/key_name`，`/stats/by-key` 与日志页「调用方」筛选可按 key 统计 token 用量
- 开关：`gateway.require_key`（yaml）或 `XT_HUB_GATEWAY__REQUIRE_KEY=false` 关闭强制鉴权（匿名调用按无 key 记账）

```bash
# OpenAI SDK 指向网关
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-xt-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'

# Anthropic SDK 指向网关（x-api-key）
curl http://localhost:8080/v1/messages \
  -H "x-api-key: sk-xt-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-sonnet","max_tokens":64,"messages":[{"role":"user","content":"你好"}]}'
```

## 渠道余额查询

- 「渠道管理」页余额列：按渠道 BaseURL 的 host 自动识别上游厂家，命中已知厂家即用渠道自身 API Key 查询其账户余额（`GET /api/v1/channels/balances` 批量返回）；
- 当前支持 **DeepSeek**（官方 `GET /user/balance`，展示总额/赠金/充值拆分，币种 CNY/USD）；其他渠道显示「—」；
- 识别规则只看 host（如 `api.deepseek.com`），路径不参与——避免把路径含厂家名的中转站误判；BaseURL 带 `/v1` 或 `/anthropic` 挂载段均自动剥到主域名根路径查询；
- 结果带 5 分钟 TTL 缓存（失败短缓存 1 分钟），拉取成功经 WS `channel.balance_updated` 实时刷新；渠道更新/删除即失效缓存；
- 智谱 Coding 套餐（窗口百分比）、小米 MiMo（仅 Cookie 鉴权）与 OpenCode Go/Zen（无公开接口）暂不支持，见「已知边界」。

## 快速开始

```bash
# 后端 + 前端一体构建（单二进制，含内嵌前端）
make build          # -> bin/xtokenhub
./bin/xtokenhub -config configs/config.yaml

# 开发模式（后端 8080 + Vite 5173 代理）
make dev

# 仅构建前端产物到 web/dist
make web
```

配置优先级：`XT_HUB_*` 环境变量 > `configs/config.yaml` > 内置默认值（如 `XT_HUB_SERVER__PORT=9090`）。

SQLite 数据落盘 `data/xtokenhub.db`（WAL 模式、单写连接）。

### 部署到 Linux 设备（systemd 开机自启）

`deploy/` 提供 systemd 单元、环境变量样例与安装脚本，用于把服务托管为开机自启的常驻进程（替代 `nohup`）。产物是全静态单文件，无需运行时依赖。

```bash
# 一键：交叉编译 linux/arm64 + 上传 + 安装单元 + 设为开机自启
make deploy-pmos XT_HOST=root@192.168.1.110

# 需要密码认证的设备（sshpass），并顺带写入出网代理
make deploy-pmos XT_HOST=root@192.168.1.110 XT_SSHPASS=xxx XT_PROXY=http://127.0.0.1:7890
```

也可手动安装（脚本幂等，重复执行即为升级）：

```bash
make dist-pmos                     # -> dist/xtokenhub-pmos-aarch64.tar.gz
scp dist/xtokenhub-pmos-aarch64.tar.gz deploy/{xtokenhub.service,xtokenhub.env.example,install.sh} <设备>:/tmp/
# 设备上：
mkdir -p /tmp/d && tar xzf /tmp/xtokenhub-pmos-aarch64.tar.gz -C /tmp/d
XT_ROOT=/path/to/XTokenHub XT_BIN=/usr/local/bin/xtokenhub XT_PROXY=http://127.0.0.1:7890 \
  sh /tmp/install.sh /tmp/d/xtokenhub-pmos-aarch64
```

安装脚本会：停掉旧的 `nohup` 进程以释放端口（只匹配 exe 指向 `xtokenhub` 的进程）→ 原子替换二进制 → 安装单元并 `enable`；检测到本机 `mihomo` 服务时自动添加启动顺序依赖。

`deploy/xtokenhub.service` 的要点：

| 项 | 说明 |
|---|---|
| `WorkingDirectory` | 指向部署根目录；`configs/config.yaml` 与 `database.path` 是相对路径，必须设对 |
| `EnvironmentFile=-/etc/default/xtokenhub` | 注入出网代理与 `XT_HUB_*` 覆盖；`-` 前缀表示文件缺失不报错 |
| `Restart=on-failure` | 异常退出自动拉起（`systemctl stop` 属正常退出，不重启） |
| `StandardOutput=journal` | 日志进 journald，替代无限增长的 `nohup.out` |
| `ProtectSystem=strict` + `ReadWritePaths=.../data` | 除 `data/` 外文件系统只读；程序只写 SQLite，无需其他写权限 |

价格表同步在首次启动（表为空）时执行一次；若设备无法直连 GitHub 而需代理，部署前先测一次可达性（同步失败不阻塞启动，但费用会保持为 0）：

```bash
curl -sS -o /dev/null -w '%{http_code}\n' https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json
```

常用运维命令：

```bash
systemctl status xtokenhub            # 状态
journalctl -u xtokenhub -f            # 实时日志（替代 tail -f nohup.out）
systemctl restart xtokenhub           # 重启
systemctl is-enabled xtokenhub        # 是否开机自启
```

### 日志保留期清理

`request_logs` 是唯一持续增长的表（每请求一行）。服务默认每 24 小时清理一次，删除超过 `max_days`（默认 90 天）的日志，也可在「请求日志」页右上角点击「清理过期日志」，或通过 `POST /api/v1/logs/cleanup` 手动触发（后台已有清理运行时返回「清理正在进行中」）。

| 配置 | 默认 | 说明 |
|---|---|---|
| `retention.enabled` | `true` | 后台定时自动清理；关闭后仍可手动触发 |
| `retention.max_days` | `90` | 保留最近 N 天日志（>0） |
| `retention.interval_hours` | `24` | 清理运行周期（小时） |
| `retention.batch_size` | `1000` | 单批删除行数（>=100，分批避免长时间占用单写连接） |
| `retention.vacuum` | `false` | 清理生效后执行 `VACUUM` 回收磁盘空间（独占锁，建议低峰开启） |

环境变量覆盖示例：`XT_HUB_RETENTION__MAX_DAYS=30`。

注意：清理后 SQLite 文件大小不会自动缩减（删除只释放页），需要开启 `vacuum` 或定时手动 `VACUUM`；仪表盘「全历史累计」等以保留期为界，热力图/按日趋势最长展示区间内的历史。费用重算同样受保留期限制（默认回看 90 天）。

### 计价相关配置

| 配置 | 默认 | 说明 |
|---|---|---|
| `pricing.enabled` | `true` | 是否启用花费统计与预测；关闭后价格表不出网、费用恒为 0 |
| `pricing.auto_sync` | `true` | 是否定时同步公开价格表 |
| `pricing.sync_interval_hours` | `24` | 同步周期（小时，>=1） |
| `pricing.source_url` | 空 | 价格表地址；留空使用内置 LiteLLM 公开价格表 |
| `pricing.timeout_seconds` | `20` | 价格表拉取超时（秒，>=1） |
| `billing.display_currency` | `USD` | 默认展示币种 `USD` / `CNY`（仅首次初始化入库，之后以设置页为准） |
| `billing.usd_cny_rate` | `0` | USD→CNY 汇率，手工维护（展示 CNY、以及折算人民币计价的价格行都需要） |
| `billing.monthly_budget_usd` | `0` | 月度预算，`0` = 不设；仅用于预测的超支提示，不拦截请求 |

价格表在首次启动（表为空）时自动同步一次；同步失败只记日志并继续启动，费用暂按 0 计，可在设置页重新点「立即同步」。

## macOS 菜单栏伴侣应用（XTokenHubMenuBar）

[`XTokenHubMenuBar/`](XTokenHubMenuBar/README.md) 是一个独立 SwiftUI 应用（类 iStat Menus，基于 macOS 26 Liquid Glass），在系统菜单栏实时展示网关 token 用量与请求流，仅消费本项目的管理 API 与 WebSocket，后端零改动：

```bash
brew install xcodegen
cd XTokenHubMenuBar && make run
```

## 测试

测试文件集中在 `internal/tests/<模块>/`（外部测试包，只测导出 API），与实现同目录解耦：

| 模块 | 手段 |
|---|---|
| pkg / config | 表驱动 + yaml/env 加载 |
| database / repository | 真实 `:memory:` SQLite（CRUD/分页/聚合/并发） |
| eventbus | 多订阅者、退订、panic 隔离 |
| ws | gorilla 真实拨号：广播、心跳、慢消费者摘除 |
| provider | httptest 假上游：探测器判定、模型列表拉取（含容错解析）、协议转换矩阵、流式 usage 解析 |
| gateway | 渠道选择（原生优先/权重/failover）、透传不改写、转换流、统计落库 |
| service / handler / router | httptest 全链路：CRUD、探测、网关 400/503/502、SPA fallback |

```bash
make test       # go test ./... -race
make cover      # 覆盖率 HTML（当前 87.6%）
cd frontend && pnpm test:run   # Vitest（WS 重连/退避、格式化、数据转换）
```

## 已知边界

- 转换路径仅支持文本对话（工具调用、多模态、cache_control 等复杂载荷请使用原生透传渠道）；
- 余额查询当前仅支持 DeepSeek（官方接口）；智谱 Coding 套餐用量为未文档化接口（返回窗口百分比）、小米 MiMo 余额仅支持网页 Cookie 鉴权且会话约一天失效、OpenCode Go/Zen 无公开余额 API，均未接入；
- 本地 token 估算为启发式（CJK ≈ 1.5 字/token，拉丁 ≈ 4 字/token），仅作上游未报告 usage 时的兜底；
- 网关 API Key 已支持（鉴权 + 按调用方统计，见「网关鉴权与按调用方统计」）；管理端 `/api/v1` 仍无登录认证，自托管内网使用场景请自行做好网络隔离；密钥明文存储（与渠道厂家 key 一致）；
- 统计缓存命中率、按 key 聚合均基于 RequestLog 快照，密钥删除后历史用量仍保留在其名称下；
- 花费为**按公开标价本地估算**，以网关自身的计量为准，与上游账单可能存在差异（缓存计费口径、阶梯价、批量折扣、赠送额度等）；**价格行缺缓存命中价时会按输入价计缓存命中，缓存命中率高的场景会高估费用**；长上下文只支持单档费率；人民币计价行的折算依赖手工维护的汇率，汇率未配置时这类模型费用记 0；错峰价的时段判定固定北京时间且不支持跨零点窗口；上游未报告 usage 而由本地估算 token 的请求，费用同为估算值。

## License

本项目基于 [MIT License](LICENSE) 开源。
