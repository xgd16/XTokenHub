import Foundation

/// XTokenHub 管理 API 客户端:统一解包 {code, message, data} 信封。
struct APIClient: Sendable {
    enum APIError: LocalizedError {
        case badResponse
        case http(Int)
        case business(String)

        var errorDescription: String? {
            switch self {
            case .badResponse: "响应格式异常"
            case .http(let code): "HTTP \(code)"
            case .business(let message): message
            }
        }
    }

    let baseURL: URL
    private let session: URLSession

    init(baseURL: URL) {
        self.baseURL = baseURL
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 15
        config.timeoutIntervalForResource = 30
        self.session = URLSession(configuration: config)
    }

    // MARK: - 端点

    /// 时间窗汇总;since(当日零点 Unix 秒)优先于 hours。
    func summary(since: Date?) async throws -> Summary {
        var query: [URLQueryItem] = [URLQueryItem(name: "hours", value: "24")]
        if let since {
            query.append(URLQueryItem(name: "since", value: String(Int64(since.timeIntervalSince1970))))
        }
        return try await get("stats/summary", query)
    }

    /// 分钟桶趋势(实时视图)。
    func trend(hours: Int, bucket: String) async throws -> [TrendPoint] {
        try await get("stats/trend", [
            URLQueryItem(name: "hours", value: String(hours)),
            URLQueryItem(name: "bucket", value: bucket),
        ])
    }

    /// 按日趋势(7 天 / 30 天视图)。
    func trend(days: Int) async throws -> [TrendPoint] {
        try await get("stats/trend", [URLQueryItem(name: "days", value: String(days))])
    }

    /// 今日按模型聚合。
    func modelTop(since: Date) async throws -> [GroupStat] {
        try await get("stats/by-model", [
            URLQueryItem(name: "hours", value: "24"),
            URLQueryItem(name: "since", value: String(Int64(since.timeIntervalSince1970))),
        ])
    }

    /// 全历史累计。
    func lifetime() async throws -> Lifetime {
        try await get("stats/lifetime", [])
    }

    /// 按调用方密钥聚合(Web 端 30 天口径为 hours=720)。
    func keyTop(hours: Int = 720) async throws -> [GroupStat] {
        try await get("stats/by-key", [URLQueryItem(name: "hours", value: String(hours))])
    }

    /// 最近请求(首屏实时流基线,后续由 WS 增量维护)。
    func logs(perPage: Int = 30) async throws -> PageData<RequestLog> {
        try await get("logs", [
            URLQueryItem(name: "page", value: "1"),
            URLQueryItem(name: "per_page", value: String(perPage)),
        ])
    }

    /// 渠道列表(面板展示接入渠道与启停状态)。
    /// 后端 per_page 上限 200,渠道更多时循环补齐。
    func channels() async throws -> [Channel] {
        var all: [Channel] = []
        var page = 1
        while page <= 50 {
            let data: PageData<Channel> = try await get("channels", [
                URLQueryItem(name: "page", value: String(page)),
                URLQueryItem(name: "per_page", value: "200"),
            ])
            all.append(contentsOf: data.items)
            if data.items.isEmpty || all.count >= data.total { break }
            page += 1
        }
        return all
    }

    /// 渠道上游账户余额(按 BaseURL 推断厂家,服务端 5 分钟 TTL 缓存)。
    func channelBalances() async throws -> ChannelBalanceList {
        try await get("channels/balances", [])
    }

    /// 花费预测;period 取 today | month。
    func costForecast(period: String) async throws -> CostForecast {
        try await get("stats/cost/forecast", [URLQueryItem(name: "period", value: period)])
    }

    /// 计费展示设置(币种/汇率/月预算)。
    func billing() async throws -> BillingSettings {
        try await get("settings/billing", [])
    }

    // MARK: - 内部

    private func get<T: Decodable & Sendable>(_ path: String, _ query: [URLQueryItem]) async throws -> T {
        guard var comps = URLComponents(url: baseURL.appending(path: "api/v1/\(path)"), resolvingAgainstBaseURL: false) else {
            throw APIError.badResponse
        }
        if !query.isEmpty { comps.queryItems = query }
        guard let url = comps.url else { throw APIError.badResponse }

        let (data, response) = try await session.data(from: url)
        if let http = response as? HTTPURLResponse, !(200..<300).contains(http.statusCode) {
            throw APIError.http(http.statusCode)
        }
        let envelope = try JSONDecoder().decode(Envelope<T>.self, from: data)
        if envelope.code != 0 {
            throw APIError.business(envelope.message.isEmpty ? "业务错误 code=\(envelope.code)" : envelope.message)
        }
        guard let payload = envelope.data else { throw APIError.badResponse }
        return payload
    }
}
