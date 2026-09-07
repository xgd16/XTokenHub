# XTokenHub

AI API 聚合网关：把各模型厂家的上游 API 汇聚成渠道，对外统一暴露三种标准协议端点，并实时统计 token 用量与缓存命中率。

**核心原则：能用原生协议就原生透传，不做格式转换；转换只作为兜底路径。**

- 后端：Go + Gin + GORM + SQLite（纯 Go 驱动 glebarez/sqlite，零 CGO）+ Viper + gorilla/websocket
- 前端：React 19 + TypeScript 7 + Ant Design 6 + Vite 8（构建产物经 go:embed 打进单二进制）
- 质量门禁：每个模块配单元测试（外部测试包），`go test ./... -race` 全绿，语句覆盖率 87.6%

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
GET    /api/v1/stats/summary?hours=24 汇总（请求数/token/缓存命中率/平均耗时/透传占比；
                                      可传 since=<unix秒> 显式指定窗口起点，前端传本地零点即「当天」口径）
GET    /api/v1/stats/trend?days=7     按日趋势（days 最长 366，供热力图/长区间）
GET    /api/v1/stats/by-model?hours=24
GET    /api/v1/stats/by-channel?hours=24
GET    /api/v1/stats/by-key?hours=24  按调用方密钥聚合（请求数/token/缓存命中率）
GET    /api/v1/stats/lifetime         全历史累计（总 token/峰值日/最长单次耗时/连续使用天数）
GET    /api/v1/stats/trend-by-model?days=7 按日 × 模型 token 用量（多模型趋势线）
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
- 统计缓存命中率、按 key 聚合均基于 RequestLog 快照，密钥删除后历史用量仍保留在其名称下。
