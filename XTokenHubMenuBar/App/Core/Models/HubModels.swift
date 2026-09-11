import Foundation

// MARK: - 通用响应包装

/// 后端统一响应信封 {code, message, data},code == 0 表示成功。
struct Envelope<T: Decodable & Sendable>: Decodable, Sendable {
    let code: Int
    let message: String
    let data: T?
}

/// 分页响应 {items, total, page, per_page}。
struct PageData<T: Decodable & Sendable>: Decodable, Sendable {
    let items: [T]
    let total: Int
    let page: Int
    let perPage: Int

    enum CodingKeys: String, CodingKey {
        case items, total, page
        case perPage = "per_page"
    }
}

// MARK: - 统计接口模型(与 internal/repository/repository.go 的 json 标签对齐)

/// GET /api/v1/stats/summary — 时间窗内汇总。
struct Summary: Decodable, Equatable, Sendable {
    var totalRequests: Int64
    var successRequests: Int64
    var errorRequests: Int64
    var promptTokens: Int64
    var completionTokens: Int64
    var totalTokens: Int64
    var cachedTokens: Int64
    /// 缓存命中率 0~1
    var cacheHitRate: Double
    var avgDurationMS: Double
    /// 原生透传占比 0~1
    var nativeRatio: Double

    enum CodingKeys: String, CodingKey {
        case totalRequests = "total_requests"
        case successRequests = "success_requests"
        case errorRequests = "error_requests"
        case promptTokens = "prompt_tokens"
        case completionTokens = "completion_tokens"
        case totalTokens = "total_tokens"
        case cachedTokens = "cached_tokens"
        case cacheHitRate = "cache_hit_rate"
        case avgDurationMS = "avg_duration_ms"
        case nativeRatio = "native_ratio"
    }
}

/// GET /api/v1/stats/trend — 趋势点;分桶查询时 ts 为桶起点 Unix 秒,按日查询只有 date。
struct TrendPoint: Decodable, Equatable, Sendable, Identifiable {
    var date: String
    var ts: Int64
    var requests: Int64
    var totalTokens: Int64
    var errorRequests: Int64

    enum CodingKeys: String, CodingKey {
        case date, ts, requests
        case totalTokens = "total_tokens"
        case errorRequests = "error_requests"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        date = try c.decode(String.self, forKey: .date)
        // Go 端 ts 带 omitempty,按日查询时整键缺失
        ts = try c.decodeIfPresent(Int64.self, forKey: .ts) ?? 0
        requests = try c.decode(Int64.self, forKey: .requests)
        totalTokens = try c.decode(Int64.self, forKey: .totalTokens)
        errorRequests = try c.decode(Int64.self, forKey: .errorRequests)
    }

    var id: String { ts > 0 ? "t\(ts)" : date }

    /// 桶起点时间;按日数据退化为本地零点。
    var pointDate: Date {
        ts > 0 ? Date(timeIntervalSince1970: TimeInterval(ts)) : HubDate.parseDay(date)
    }
}

/// GET /api/v1/stats/by-model 等聚合端点共用。
struct GroupStat: Decodable, Equatable, Sendable, Identifiable {
    var name: String
    var requests: Int64
    var totalTokens: Int64
    var cachedTokens: Int64
    var cacheRate: Double
    var avgMS: Double

    enum CodingKeys: String, CodingKey {
        case name, requests
        case totalTokens = "total_tokens"
        case cachedTokens = "cached_tokens"
        case cacheRate = "cache_rate"
        case avgMS = "avg_ms"
    }

    var id: String { name }
}

/// GET /api/v1/stats/lifetime — 全历史累计。
struct Lifetime: Decodable, Equatable, Sendable {
    var totalTokens: Int64
    var peakDayTokens: Int64
    var peakDay: String
    var maxDurationMS: Int64
    var currentStreak: Int
    var maxStreak: Int
    var activeDays: Int

    enum CodingKeys: String, CodingKey {
        case totalTokens = "total_tokens"
        case peakDayTokens = "peak_day_tokens"
        case peakDay = "peak_day"
        case maxDurationMS = "max_duration_ms"
        case currentStreak = "current_streak"
        case maxStreak = "max_streak"
        case activeDays = "active_days"
    }
}

// MARK: - 请求日志(与 internal/model/request_log.go 对齐)

/// GET /api/v1/logs 行数据,同时也是 WS request.started/completed 的载荷。
struct RequestLog: Decodable, Equatable, Identifiable, Sendable {
    /// 落库行主键;WS 实时推送的"进行中"行尚未落库,为 0。
    var logID: Int64
    /// 进程内请求序号,started/completed 事件据此配对。
    var reqID: Int64
    var createdAt: String
    var protocolName: String
    var forwardMode: String
    var channelID: Int64
    var channelName: String
    var keyID: Int64
    var keyName: String
    var model: String
    var stream: Bool
    var promptTokens: Int64
    var completionTokens: Int64
    var totalTokens: Int64
    var cachedTokens: Int64
    var cacheWriteTokens: Int64
    var cacheHitRate: Double
    /// 本请求费用(USD),进行中行为 0;旧后端缺键时为 nil。
    var costUSD: Double?
    /// 计价所用上游 usage 口径(openai | anthropic)。
    var usageStyle: String?
    /// 计价命中的时段(peak | off_peak);模型未配置时段价时为空。
    var pricePeriod: String?
    var durationMS: Int64
    var upstreamStatus: Int
    var clientIP: String
    var userAgent: String
    var sessionID: String
    var requestHeaders: String
    var error: String

    enum CodingKeys: String, CodingKey {
        case logID = "id"
        case reqID = "req_id"
        case createdAt = "created_at"
        case protocolName = "protocol"
        case forwardMode = "forward_mode"
        case channelID = "channel_id"
        case channelName = "channel_name"
        case keyID = "key_id"
        case keyName = "key_name"
        case model, stream
        case promptTokens = "prompt_tokens"
        case completionTokens = "completion_tokens"
        case totalTokens = "total_tokens"
        case cachedTokens = "cached_tokens"
        case cacheWriteTokens = "cache_write_tokens"
        case cacheHitRate = "cache_hit_rate"
        case costUSD = "cost_usd"
        case usageStyle = "usage_style"
        case pricePeriod = "price_period"
        case durationMS = "duration_ms"
        case upstreamStatus = "upstream_status"
        case clientIP = "client_ip"
        case userAgent = "user_agent"
        case sessionID = "session_id"
        case requestHeaders = "request_headers"
        case error
    }

    /// 是否仍在进行中(WS 推送、未落库)。
    var isPending: Bool { logID == 0 && reqID > 0 }
    var isError: Bool { !error.isEmpty || (upstreamStatus >= 400) }
    var loggedAt: Date? { HubDate.parseRFC3339(createdAt) }

    /// 实时行按 req_id 配对原位替换,落库行按 id 区分。
    var id: String {
        reqID > 0 ? "req-\(reqID)" : "log-\(logID)-\(createdAt)"
    }
}

// MARK: - 渠道(与 internal/model/channel.go 对齐)

/// GET /api/v1/channels 行数据。
///
/// 后端会返回明文 api_key,这里刻意不声明该字段(JSONDecoder 忽略未知键),
/// 避免密钥进入内存或日志。
struct Channel: Decodable, Equatable, Identifiable, Sendable {
    var id: Int64
    var name: String
    var provider: String
    var baseURL: String
    /// 1 = 启用,0 = 停用。
    var status: Int
    var priority: Int
    var remark: String

    enum CodingKeys: String, CodingKey {
        case id, name, provider, status, priority, remark
        case baseURL = "base_url"
    }

    var isEnabled: Bool { status == 1 }
}

/// 上游账户余额快照(GET /api/v1/channels/balances 内嵌)。
struct BalanceInfo: Decodable, Equatable, Sendable {
    var provider: String
    var isAvailable: Bool
    var currency: String
    var total: Double
    /// 赠金。
    var granted: Double
    /// 充值。
    var toppedUp: Double
    var fetchedAt: String

    enum CodingKeys: String, CodingKey {
        case provider, currency, total, granted
        case isAvailable = "is_available"
        case toppedUp = "topped_up"
        case fetchedAt = "fetched_at"
    }
}

/// 单渠道余额查询结果;supported == false 表示该 BaseURL 无对应余额接口。
struct ChannelBalance: Decodable, Equatable, Identifiable, Sendable {
    var channelID: Int64
    var channelName: String
    var provider: String
    var supported: Bool
    var ok: Bool
    var balance: BalanceInfo?
    var error: String?
    var fetchedAt: String?

    enum CodingKeys: String, CodingKey {
        case provider, supported, ok, balance, error
        case channelID = "channel_id"
        case channelName = "channel_name"
        case fetchedAt = "fetched_at"
    }

    var id: Int64 { channelID }
}

/// GET /api/v1/channels/balances 响应体。
struct ChannelBalanceList: Decodable, Equatable, Sendable {
    var items: [ChannelBalance]
}

// MARK: - 花费(与 internal/service/forecast.go 对齐)

/// GET /api/v1/stats/cost/forecast 花费预测。
struct CostForecast: Decodable, Equatable, Sendable {
    /// today | month。
    var period: String
    var spentUSD: Double
    /// nil 表示样本不足,不给出预测(见 reason)。
    var projectedUSD: Double?
    var burnPerHourUSD: Double
    /// 近 7 个完整日的日均花费。
    var dailyAvgUSD: Double
    /// run_rate(近 7 日日均) | linear(本周期线性外推)。
    var basis: String
    /// low | medium | high。
    var confidence: String
    var reason: String?
    /// 月度预算(0 = 未设)。
    var budgetUSD: Double
    var projectedExceededDate: String?

    enum CodingKeys: String, CodingKey {
        case period, basis, confidence, reason
        case spentUSD = "spent_usd"
        case projectedUSD = "projected_usd"
        case burnPerHourUSD = "burn_per_hour_usd"
        case dailyAvgUSD = "daily_avg_usd"
        case budgetUSD = "budget_usd"
        case projectedExceededDate = "projected_exceeded_date"
    }
}

/// 计费展示设置(GET /api/v1/settings/billing)。
struct BillingSettings: Decodable, Equatable, Sendable {
    /// USD | CNY。
    var displayCurrency: String
    /// USD -> CNY 汇率,仅在展示币种为 CNY 时生效。
    var usdRate: Double
    var monthlyBudgetUSD: Double

    enum CodingKeys: String, CodingKey {
        case displayCurrency = "display_currency"
        case usdRate = "usd_cny_rate"
        case monthlyBudgetUSD = "monthly_budget_usd"
    }
}

// MARK: - WS 载荷

/// stats.throughput 事件载荷(2Hz 推送)。
struct ThroughputPayload: Decodable, Equatable, Sendable {
    var tokensPerSec: Double
    var activeStreams: Int

    enum CodingKeys: String, CodingKey {
        case tokensPerSec = "tokens_per_sec"
        case activeStreams = "active_streams"
    }
}

/// channel.balance_updated 事件载荷:后端拉取成功后推送,面板直接合并。
struct ChannelBalancePayload: Decodable, Equatable, Sendable {
    var channelID: Int64
    var channelName: String
    var balance: BalanceInfo

    enum CodingKeys: String, CodingKey {
        case balance
        case channelID = "channel_id"
        case channelName = "channel_name"
    }
}
