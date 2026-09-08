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
