import Foundation
import WidgetKit

/// Widget 数据访问层。
///
/// 取数顺序（任一成功即返回）：
/// 1. **主 App 的本机回环端点** `http://127.0.0.1:9193/snapshot`
///    —— 能拿到主 App 当前选中的数据来源与已算好的统计，零配置；
/// 2. Widget 配置里手填的服务地址，直接请求 XTokenHub REST 接口；
/// 3. 本地缓存（过期也照常展示，只标记为陈旧）。
///
/// 之所以不用 App Groups：本项目 ad-hoc 签名（无开发者账号），
/// `com.apple.security.application-groups` 是受限权限，构建期即被拒绝。
enum WidgetDataProvider {
    static let widgetKind = "XTokenHubWidget"

    /// 服务地址默认值与主 App 的默认数据来源保持一致。
    static let defaultBaseURL = "http://127.0.0.1:9192"
    static let defaultSourceName = "XTokenHub"

    // MARK: - Widget 本地配置与缓存

    private enum Keys {
        static let summary = "widget_cache_summary"
        static let costToday = "widget_cache_cost_today"
        static let modelTop = "widget_cache_model_top"
        static let channelBalances = "widget_cache_channel_balances"
        static let trend = "widget_cache_trend"
        static let sourceName = "widget_cache_source_name"
        static let billingCurrency = "widget_cache_billing_currency"
        static let billingRate = "widget_cache_billing_rate"
        static let timestamp = "widget_cache_timestamp"
    }

    /// 缓存有效期：超过后尝试联网刷新，联网失败则继续展示旧数据。
    static let cacheValidDuration: TimeInterval = 300

    private static var defaults: UserDefaults { .standard }

    // MARK: - 数据快照

    struct WidgetSnapshot: Sendable {
        var summary: Summary?
        var costToday: CostForecast?
        var modelTop: [GroupStat]
        var channelBalances: [ChannelBalance]
        /// 近 24 小时按小时分桶的趋势。
        var trend: [TrendPoint]
        var sourceName: String
        /// 花费展示币种与汇率（与主 App 计费设置同口径）。
        var billingCurrency: String
        var billingRate: Double
        var timestamp: Date
        var source: DataSource

        enum DataSource: Sendable {
            /// 来自主 App 桥接端点。
            case appBridge
            case network
            /// 来自本地缓存且仍在有效期内。
            case cache
            /// 从未成功拉取过数据。
            case noData
            /// 联网失败但存在历史缓存。
            case staleCache
        }

        /// 花费格式化选项；CNY 但汇率为 0 时 `MoneyFormatter` 内部会回退 USD。
        var moneyOptions: MoneyFormatter.Options {
            MoneyFormatter.Options(currency: billingCurrency, rate: billingRate)
        }

        static let empty = WidgetSnapshot(
            summary: nil,
            costToday: nil,
            modelTop: [],
            channelBalances: [],
            trend: [],
            sourceName: WidgetDataProvider.defaultSourceName,
            billingCurrency: "USD",
            billingRate: 0,
            timestamp: .distantPast,
            source: .noData
        )

        var isStale: Bool {
            Date().timeIntervalSince(timestamp) > WidgetDataProvider.cacheValidDuration
        }
    }

    // MARK: - 主 App 调用：推动 Widget 刷新

    /// 通知系统重新生成 Widget 时间线（主 App 数据变化时调用）。
    static func reloadWidgets() {
        WidgetCenter.shared.reloadTimelines(ofKind: widgetKind)
    }

    // MARK: - Widget 调用：读取快照

    /// - Parameter configuredURL: Widget 配置里填写的服务地址，留空则只用桥接与缓存。
    static func loadSnapshot(configuredURL: String?) async -> WidgetSnapshot {
        // 1) 主 App 桥接（零配置，且能用上主 App 当前选中的数据来源）
        if let bridged = await fetchFromAppBridge(configURL: configuredURL) {
            return bridged
        }

        // 2) 缓存仍然新鲜则直接用，省一次请求
        if let cached = readCache(), !cached.isStale {
            return cached
        }

        // 3) Widget 自己请求 XTokenHub 接口
        let urlString = configuredURL.flatMap { $0.isEmpty ? nil : $0 } ?? defaultBaseURL
        if let direct = await fetchDirect(urlString: urlString) {
            return direct
        }

        // 4) 兜底：过期缓存
        if let cached = readCache() {
            var stale = cached
            stale.source = .staleCache
            return stale
        }
        return .empty
    }

    // MARK: - 数据来源 1：主 App 桥接端点

    private static func fetchFromAppBridge(configURL: String?) async -> WidgetSnapshot? {
        var request = URLRequest(url: WidgetBridge.url)
        request.timeoutInterval = 4
        request.cachePolicy = .reloadIgnoringLocalCacheData

        guard let (data, response) = try? await URLSession.shared.data(for: request),
              let http = response as? HTTPURLResponse, http.statusCode == 200,
              let payload = try? decoder.decode(WidgetBridgePayload.self, from: data) else {
            return nil
        }

        var snapshot = WidgetSnapshot(
            summary: payload.summary,
            costToday: payload.costToday,
            modelTop: payload.modelTop,
            channelBalances: payload.channelBalances,
            trend: [],
            sourceName: payload.sourceName,
            billingCurrency: payload.billingCurrency,
            billingRate: payload.billingRate,
            timestamp: payload.generatedAt,
            source: .appBridge
        )

        // 趋势主 App 未缓存，用同一地址单独取一次（失败不影响其余数据）
        if let url = URL(string: normalize(payload.baseURL)) {
            snapshot.trend = (try? await APIClient(baseURL: url).trend(hours: 24, bucket: "hour")) ?? []
        }

        writeCache(snapshot)
        return snapshot
    }

    // MARK: - 数据来源 3：直接请求 XTokenHub

    private static func fetchDirect(urlString: String) async -> WidgetSnapshot? {
        guard let url = URL(string: normalize(urlString)) else { return nil }
        let api = APIClient(baseURL: url)
        let midnight = Calendar.current.startOfDay(for: Date())

        do {
            async let summaryTask = api.summary(since: midnight)
            async let modelTask = api.modelTop(since: midnight)
            async let costTask = api.costForecast(period: "today")
            async let trendTask = api.trend(hours: 24, bucket: "hour")

            let (summary, models, cost, trend) = try await (summaryTask, modelTask, costTask, trendTask)

            // 计费设置失败时回退 USD，不拖垮主数据
            let billing = try? await api.billing()

            var snapshot = WidgetSnapshot(
                summary: summary,
                costToday: cost,
                modelTop: Array(models.prefix(5)),
                channelBalances: [],
                trend: trend,
                sourceName: displayName(for: url),
                billingCurrency: billing?.displayCurrency ?? "USD",
                billingRate: billing?.usdRate ?? 0,
                timestamp: Date(),
                source: .network
            )

            // 余额接口较慢且非核心，失败不影响主数据
            if let balances = try? await api.channelBalances() {
                snapshot.channelBalances = balances.items
            }

            writeCache(snapshot)
            return snapshot
        } catch {
            return nil
        }
    }

    /// 未配置显示名时用主机名代替，例如 `192.168.1.110`。
    private static func displayName(for url: URL) -> String {
        guard let host = url.host else { return defaultSourceName }
        return host == "127.0.0.1" || host == "localhost" ? "本地" : host
    }

    // MARK: - 缓存

    static func clearCache() {
        [Keys.summary, Keys.costToday, Keys.modelTop, Keys.channelBalances,
         Keys.trend, Keys.sourceName, Keys.billingCurrency, Keys.billingRate,
         Keys.timestamp].forEach { defaults.removeObject(forKey: $0) }
    }

    private static func readCache() -> WidgetSnapshot? {
        let ts = defaults.double(forKey: Keys.timestamp)
        guard ts > 0 else { return nil }

        let decoder = JSONDecoder()
        return WidgetSnapshot(
            summary: defaults.data(forKey: Keys.summary)
                .flatMap { try? decoder.decode(Summary.self, from: $0) },
            costToday: defaults.data(forKey: Keys.costToday)
                .flatMap { try? decoder.decode(CostForecast.self, from: $0) },
            modelTop: defaults.data(forKey: Keys.modelTop)
                .flatMap { try? decoder.decode([GroupStat].self, from: $0) } ?? [],
            channelBalances: defaults.data(forKey: Keys.channelBalances)
                .flatMap { try? decoder.decode([ChannelBalance].self, from: $0) } ?? [],
            trend: defaults.data(forKey: Keys.trend)
                .flatMap { try? decoder.decode([TrendPoint].self, from: $0) } ?? [],
            sourceName: defaults.string(forKey: Keys.sourceName) ?? defaultSourceName,
            billingCurrency: defaults.string(forKey: Keys.billingCurrency) ?? "USD",
            billingRate: defaults.double(forKey: Keys.billingRate),
            timestamp: Date(timeIntervalSince1970: ts),
            source: .cache
        )
    }

    private static func writeCache(_ snapshot: WidgetSnapshot) {
        let encoder = JSONEncoder()
        defaults.set(snapshot.timestamp.timeIntervalSince1970, forKey: Keys.timestamp)
        defaults.set(snapshot.sourceName, forKey: Keys.sourceName)
        defaults.set(snapshot.billingCurrency, forKey: Keys.billingCurrency)
        defaults.set(snapshot.billingRate, forKey: Keys.billingRate)

        if let summary = snapshot.summary, let data = try? encoder.encode(summary) {
            defaults.set(data, forKey: Keys.summary)
        }
        if let cost = snapshot.costToday, let data = try? encoder.encode(cost) {
            defaults.set(data, forKey: Keys.costToday)
        }
        if !snapshot.modelTop.isEmpty, let data = try? encoder.encode(snapshot.modelTop) {
            defaults.set(data, forKey: Keys.modelTop)
        }
        if !snapshot.channelBalances.isEmpty, let data = try? encoder.encode(snapshot.channelBalances) {
            defaults.set(data, forKey: Keys.channelBalances)
        }
        if !snapshot.trend.isEmpty, let data = try? encoder.encode(snapshot.trend) {
            defaults.set(data, forKey: Keys.trend)
        }
    }

    private static var decoder: JSONDecoder { JSONDecoder() }

    /// 补全协议前缀并去掉尾部斜杠。
    private static func normalize(_ raw: String) -> String {
        var s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.isEmpty { return defaultBaseURL }
        if !s.contains("://") { s = "http://" + s }
        while s.hasSuffix("/") { s.removeLast() }
        return s
    }
}
