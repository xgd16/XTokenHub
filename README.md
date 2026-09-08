<div align="center">

<img src="docs/logo.png" width="96" alt="XTokenHub Logo"/>

# XTokenHub

English | [简体中文](README.zh-CN.md)

**AI API aggregation gateway · Unified token management for all your models**

Bring together upstream LLM provider APIs as channels, expose standardized protocol endpoints, and track token usage and cache hit rate in real time.

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-1.x-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)
![SQLite](https://img.shields.io/badge/SQLite-zero%20CGO-003B57?logo=sqlite&logoColor=white)
![Test](https://img.shields.io/badge/coverage-87.6%25-brightgreen)

</div>

---

**Core principle: pass through in the provider's native protocol whenever possible; conversion is only a fallback.**

| Dashboard | Channel Management |
|---|---|
| ![Dashboard](docs/screenshot-dashboard.png) | ![Channels](docs/screenshot-channels.png) |
| **Model Usage** | **API Keys** |
| ![Model Usage](docs/screenshot-model-usage.png) | ![API Keys](docs/screenshot-apikeys.png) |
| **macOS Menu Bar Companion** | |
| <img src="docs/screenshot-menubar.png" width="360" alt="macOS menu bar app"/> | |

## ✨ Highlights

- **🔑 Unified token management** — Gather all your scattered provider API keys into one panel: channel-based entry, one-click availability probing, enable/disable switching, and balance queries (e.g. DeepSeek). No more digging through config files for keys;
- **🗂 Model grouping** — Each channel carries its own model list (one-click pull from upstream), and multiple upstreams are aggregated into a single `/v1/models` view. Route by priority / weighted random with automatic failover across channels serving the same model;
- **📊 Instant usage insight** — The dashboard shows request count, token usage, cache hit rate, and average latency in real time, aggregated by model / channel / caller key, with a GitHub-style token activity heatmap and daily trend charts, all pushed live over WebSocket;
- **🔄 Protocol conversion** — Exposes OpenAI (`/v1/chat/completions`, `/v1/responses`) and Anthropic (`/v1/messages`) endpoints simultaneously; when inbound and upstream protocols differ, requests are converted automatically. When they match, traffic is passed through natively with zero conversion, preserving tool calls, multimodal payloads, and other full capabilities;
- **🧾 Gateway keys & per-caller stats** — Issue `sk-xt-*` gateway keys to different clients and aggregate request count and tokens per key, so you always know who consumes the most;
- **📦 Single-binary self-hosting** — The frontend is embedded via go:embed; `make build` produces one static binary (zero CGO). Copy it to any Linux/macOS machine and run.

**Tech stack**: Backend Go + Gin + GORM + SQLite (pure-Go driver glebarez/sqlite) + Viper + gorilla/websocket; frontend React 19 + TypeScript + Ant Design 6 + Vite. Every module has unit tests — `go test ./... -race` all green, 87.6% statement coverage.

## Architecture

```
cmd/server/          Entry: config -> DB -> event bus -> WS hub -> router -> graceful shutdown
internal/
  config/            Viper config (defaults <- yaml <- XT_HUB_ env vars)
  database/          GORM + SQLite init and AutoMigrate
  model/             Channel / RequestLog and protocol enums
  repository/        Data access interfaces + GORM impl (interface-based, services mockable)
  service/           Business logic: channel CRUD, protocol probing, logs, stats
  handler/
    admin/           Admin API (channels / logs / stats / WS)
    gateway/         Gateway endpoints (parse inbound request -> executor)
  gateway/           Orchestration: channel selection (native-first) + passthrough/convert + streaming + stats
  provider/          Upstream integration: endpoint resolution, native-protocol prober, converters, usage parsing, token estimation
  eventbus/          In-memory pub/sub bus (decouples business from push)
  ws/                WebSocket hub: connection management, heartbeat keep-alive, async broadcast, slow-consumer eviction
  middleware/        CORS / access log / recovery
  router/            Route registration + frontend SPA hosting (except /api, /v1)
  pkg/               resp unified response / errs error codes / pagination / logger
web/web.go           go:embed frontend bundle (web/dist)
frontend/            React admin source (dashboard / channels / request log / model usage)
internal/tests/      All unit tests (mirrors module layout, external test packages)
```

## Gateway Workflow

1. **Create a channel**: enter provider BaseURL / API key / model list (or click "pull from upstream" to fetch the real model list). The API style (OpenAI-compatible Bearer / Anthropic-compatible x-api-key) is auto-detected from the BaseURL (hostname or path containing "anthropic" means Anthropic style) and can be overridden. Create one channel per protocol endpoint of the same provider — e.g. DeepSeek: OpenAI face `https://api.deepseek.com`, Anthropic face `https://api.deepseek.com/anthropic` (the gateway assembles `.../anthropic/v1/messages`). BaseURL supports three mounting forms: bare domain (endpoints get `/v1/`), `/v1` suffix (equivalent to bare domain), sub-path mount (e.g. `/anthropic`, assembles `/v1/messages`), and versioned mount (e.g. Zhipu `https://open.bigmodel.cn/api/paas/v4`, assembles `.../v4/chat/completions` directly without duplicating the version segment). The model list prefers the channel's own `.../models`; if unavailable (e.g. DeepSeek sub-paths expose no list endpoint), it falls back to the main domain `/v1/models`;
2. **Native protocol probing**: send a minimal request (max_tokens=1) to each protocol endpoint; a 2xx marks it a native protocol of the channel (401/404/5xx don't count; `native_protocols` can be corrected manually). Bare-domain BaseURLs probe all three protocols (to detect multi-protocol relays); sub-path BaseURLs (like DeepSeek's `https://api.deepseek.com/anthropic`) are treated as protocol mounts and only probe that provider's protocol family. If no probe model is specified, a real one is picked automatically: upstream's own model list -> channel config models -> main-domain fallback list -> built-in fallback, avoiding false verdicts on nonexistent models;
3. **Request routing**: filter enabled channels by model -> **prefer native channels (zero-conversion passthrough)** -> only fall back to protocol conversion if no native channel exists -> priority ascending + weighted random by weight -> automatic failover on failure (network errors / 401 / 403 / 408 / 429 / 5xx);
4. **Metering & stats**: parse upstream usage first (`stream_options.include_usage` is added automatically for OpenAI streaming; Anthropic uses `message_start`/`message_delta` events); when upstream doesn't report usage, a local heuristic estimation kicks in. Cache hit rate is computed from `cached_tokens` (OpenAI) / `cache_read_input_tokens` (Anthropic); every request is written to `request_logs` and pushed to the frontend in real time via WebSocket.

Protocol conversion matrix (fallback path; v1 focuses on text chat + common sampling params; tool calls / multimodal are only guaranteed on the passthrough path):

| Inbound \ Upstream | openai_compatible | anthropic |
|---|---|---|
| chat_completions | native passthrough | convert |
| responses | convert | convert |
| messages | convert | native passthrough |

> Converting upstreams don't support the responses protocol (the OpenAI Responses API is semantically heavy; native channels take priority).

## WebSocket Real-time Push

- Endpoint: `GET /api/v1/ws`; message envelope `{type, payload, ts}`
- Events: `request.completed` (every gateway request), `stats.updated`, `channel.probe_result`, `channel.status_changed`, `channel.balance_updated`
- Business logic only publishes to the eventbus; the WS hub subscribes and broadcasts — business and push are fully decoupled; server-side ping/pong keep-alive, automatic eviction of slow consumers
- Frontend `src/api/ws.ts`: exponential backoff reconnection (starting at 1s, capped at 30s), subscribe by type, connection state drives the header breathing light

## Dashboard

- **Today scope**: top stat cards count requests/tokens/cache hit rate for "today" (from local midnight, via the `since` parameter) rather than a rolling 24h window;
- **Lifetime cards**: total tokens, peak tokens (single-day high), longest chat duration (single successful request), current/longest consecutive usage days (GitHub-streak style, derived from per-day aggregation);
- **Token activity heatmap**: GitHub-style calendar (last 26 weeks, aggregated by local day), with daily / weekly / cumulative coloring modes, shown alongside "Top callers · 30 days" (requests and tokens aggregated by gateway key);
- **Daily token trend**: switch between last 7 / 30 days, smoothed multi-model curves (day × model aggregation, top 6 + others);
- **Model usage donut**: same data source and palette as the trend chart, total tokens for the range in the center, legend with share and usage;
- **Chart interactions**: hovering the trend chart shows crosshairs and per-model breakdowns for the day; hovering the donut boldens the sector, switches the center to its share, and highlights the legend; hovering the heatmap shows that day's tokens;
- Real-time request stream: rows are merged into sessions by caller (`X-Session-Id` inbound header, stored as `session_id`), summarizing request count / tokens / cache hit / latency; click a row's arrow to expand request-level details. Callers without a session header stay on a single row; each row has a "view headers" action showing the full inbound request headers (`request_headers`).

## API

Admin endpoints (`/api/v1`, no login auth yet):

```
GET    /api/v1/ws                     WebSocket real-time push
GET    /api/v1/channels               Channel list (paginated)
POST   /api/v1/channels               Create channel
POST   /api/v1/channels/lookup-models Pull upstream model list {provider, base_url, api_key}
GET    /api/v1/channels/balances      Batch channel balance query (provider detected by BaseURL host;
                                      currently DeepSeek only, 5-minute TTL cache + WS push)
GET    /api/v1/channels/:id
PUT    /api/v1/channels/:id
DELETE /api/v1/channels/:id
POST   /api/v1/channels/:id/probe     Trigger native protocol probe {model?} (auto-picked from upstream model list by default)
GET    /api/v1/keys                   Gateway key list (paginated)
POST   /api/v1/keys                   Create key (server generates sk-xt-*; response contains the plaintext once)
PUT    /api/v1/keys/:id               Rename / enable-disable / note (key body is immutable)
DELETE /api/v1/keys/:id
GET    /api/v1/logs                   Request logs (protocol/forward_mode/channel_id/key_id/model/stream/hours/error_only filters)
GET    /api/v1/logs/cleanup           Log cleanup status (enabled/retention days/last run info)
POST   /api/v1/logs/cleanup           Trigger a cleanup manually (returns deleted rows/duration/cutoff)
GET    /api/v1/stats/summary?hours=24 Summary (requests/tokens/cache hit rate/avg latency/passthrough share;
                                      pass since=<unix seconds> for an explicit window start; the frontend
                                      passes local midnight for the "today" scope)
GET    /api/v1/stats/trend?days=7     Daily trend (days up to 366, for heatmap/long ranges)
GET    /api/v1/stats/by-model?hours=24
GET    /api/v1/stats/by-channel?hours=24
GET    /api/v1/stats/by-key?hours=24  Aggregate by caller key (requests/tokens/cache hit rate)
GET    /api/v1/stats/lifetime         Lifetime totals (total tokens/peak day/longest single duration/consecutive days)
GET    /api/v1/stats/trend-by-model?days=7 Day × model token usage (multi-model trend lines)
GET    /healthz
```

Gateway endpoints (OpenAI / Anthropic clients connect directly, API key enforced by default):

```
GET  /v1/models            Aggregated model list of all enabled channels (OpenAI spec format,
                           plus Anthropic's type/display_name fields for dev-tool recognition)
POST /v1/chat/completions
POST /v1/responses
POST /v1/messages
```

### Gateway auth & per-caller stats

- Create keys on the "API Keys" page (`sk-xt-` + 32 random hex chars); auto-copied to clipboard on creation; supports enable/disable and delete (historical logs keep the key-name snapshot)
- Caller auth: `Authorization: Bearer sk-xt-...` (OpenAI-compatible) or `x-api-key: sk-xt-...` (Anthropic-compatible); missing/wrong/disabled keys return 401 (formatted per the inbound protocol)
- Every request log records `key_id/key_name`; `/stats/by-key` and the log page's "caller" filter let you aggregate token usage per key
- Toggle: `gateway.require_key` (yaml) or `XT_HUB_GATEWAY__REQUIRE_KEY=false` to disable enforced auth (anonymous calls are accounted under no key)

```bash
# Point an OpenAI SDK at the gateway
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-xt-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'

# Point an Anthropic SDK at the gateway (x-api-key)
curl http://localhost:8080/v1/messages \
  -H "x-api-key: sk-xt-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-sonnet","max_tokens":64,"messages":[{"role":"user","content":"Hello"}]}'
```

## Channel Balance Queries

- The balance column on the "Channel Management" page: the provider is detected from the channel's BaseURL host; a known provider is queried with the channel's own API key (`GET /api/v1/channels/balances` returns in batch);
- Currently supports **DeepSeek** only (official `GET /user/balance`, showing total/grant/top-up breakdown, CNY/USD); other channels show "—";
- Detection uses only the host (e.g. `api.deepseek.com`) — paths are ignored to avoid misjudging relays whose path mentions a provider name; BaseURLs with `/v1` or `/anthropic` mount segments are stripped to the main domain root for the query;
- Results carry a 5-minute TTL cache (short 1-minute cache on failure); successful fetches refresh live via WS `channel.balance_updated`; updating/deleting a channel invalidates the cache;
- Zhipu Coding plans (window percentage), Xiaomi MiMo (Cookie auth only), and OpenCode Go/Zen (no public API) are not yet supported — see "Known Limitations".

## Quick Start

```bash
# Backend + frontend in one build (single binary with embedded frontend)
make build          # -> bin/xtokenhub
./bin/xtokenhub -config configs/config.yaml

# Dev mode (backend 8080 + Vite 5173 proxy)
make dev

# Build only the frontend bundle into web/dist
make web
```

Config precedence: `XT_HUB_*` env vars > `configs/config.yaml` > built-in defaults (e.g. `XT_HUB_SERVER__PORT=9090`).

SQLite data lands in `data/xtokenhub.db` (WAL mode, single writer connection).

### Log retention cleanup

`request_logs` is the only table that grows forever (one row per request). The service cleans up every 24 hours by default, deleting logs older than `max_days` (default 90). You can also click "Clean expired logs" on the "Request Logs" page, or trigger manually via `POST /api/v1/logs/cleanup` (returns "cleanup in progress" if one is already running).

| Setting | Default | Description |
|---|---|---|
| `retention.enabled` | `true` | Automatic background cleanup; manual trigger still works when off |
| `retention.max_days` | `90` | Keep the most recent N days of logs (>0) |
| `retention.interval_hours` | `24` | Cleanup cycle (hours) |
| `retention.batch_size` | `1000` | Rows deleted per batch (>=100, batched to avoid holding the single writer connection) |
| `retention.vacuum` | `false` | Run `VACUUM` after cleanup to reclaim disk (exclusive lock; enable at off-peak hours) |

Env override example: `XT_HUB_RETENTION__MAX_DAYS=30`.

Note: the SQLite file doesn't shrink automatically after cleanup (deletion only frees pages); enable `vacuum` or run `VACUUM` manually. Lifetime dashboard stats are bounded by the retention period; the heatmap/daily trends show history within their display ranges.

## macOS Menu Bar Companion (XTokenHubMenuBar)

[`XTokenHubMenuBar/`](XTokenHubMenuBar/README.md) is a standalone SwiftUI app (iStat Menus-style, built on macOS 26 Liquid Glass) that shows gateway token usage and the live request stream in the system menu bar. It only consumes this project's admin API and WebSocket — zero backend changes:

```bash
brew install xcodegen
cd XTokenHubMenuBar && make run
```

## Testing

Tests live in `internal/tests/<module>/` (external test packages, exported APIs only), decoupled from the implementation directory:

| Module | Approach |
|---|---|
| pkg / config | Table-driven + yaml/env loading |
| database / repository | Real `:memory:` SQLite (CRUD/pagination/aggregation/concurrency) |
| eventbus | Multiple subscribers, unsubscribe, panic isolation |
| ws | Real gorilla dials: broadcast, heartbeat, slow-consumer eviction |
| provider | httptest fake upstreams: prober verdicts, model-list fetching (tolerant parsing), conversion matrix, streaming usage parsing |
| gateway | Channel selection (native-first/weight/failover), passthrough non-rewriting, conversion streams, stats persistence |
| service / handler / router | httptest end-to-end: CRUD, probing, gateway 400/503/502, SPA fallback |

```bash
make test       # go test ./... -race
make cover      # coverage HTML (currently 87.6%)
cd frontend && pnpm test:run   # Vitest (WS reconnect/backoff, formatting, data transforms)
```

## Known Limitations

- The conversion path supports text chat only (tool calls, multimodal, cache_control and other complex payloads require native passthrough channels);
- Balance queries currently support DeepSeek only (official API); Zhipu Coding plan usage is an undocumented API (returns window percentage), Xiaomi MiMo balance requires web-cookie auth that expires in about a day, and OpenCode Go/Zen expose no public balance API — none are integrated;
- Local token estimation is heuristic (CJK ≈ 1.5 chars/token, Latin ≈ 4 chars/token), only as a fallback when upstreams don't report usage;
- Gateway API keys are supported (auth + per-caller stats, see above); the admin `/api/v1` still has no login auth — for self-hosted intranet use, apply your own network isolation. Keys are stored in plaintext (same as provider keys);
- Cache hit rate and per-key aggregation are based on RequestLog snapshots; after a key is deleted, its historical usage remains under its name.

## License

Released under the [MIT License](LICENSE).
