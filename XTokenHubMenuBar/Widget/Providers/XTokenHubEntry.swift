import Foundation
import WidgetKit

/// Widget Timeline Entry — 一次渲染所需的全部数据。
struct XTokenHubEntry: TimelineEntry {
    let date: Date
    let snapshot: WidgetDataProvider.WidgetSnapshot

    var summary: Summary? { snapshot.summary }
    var costToday: CostForecast? { snapshot.costToday }
    var modelTop: [GroupStat] { snapshot.modelTop }
    var channelBalances: [ChannelBalance] { snapshot.channelBalances }
    var trend: [TrendPoint] { snapshot.trend }
    var sourceName: String { snapshot.sourceName }

    /// 趋势图用的 Token 数值序列。
    var trendValues: [Double] { trend.map { Double($0.totalTokens) } }

    /// 数据是否可用（新鲜或过期缓存都算“有数据”，仅无数据时为 false）。
    var hasData: Bool { snapshot.summary != nil }

    /// 连接状态：桥接/直连成功，或缓存仍新鲜，都视为在线。
    var isConnected: Bool {
        switch snapshot.source {
        case .appBridge, .network: return true
        case .cache: return !snapshot.isStale
        case .staleCache, .noData: return false
        }
    }

    // MARK: - 占位数据（Widget Gallery / 预览）

    static func placeholder() -> XTokenHubEntry {
        XTokenHubEntry(date: Date(), snapshot: WidgetSnapshotFactory.sample)
    }

    static func noData() -> XTokenHubEntry {
        XTokenHubEntry(date: Date(), snapshot: .empty)
    }
}

/// 示例数据集中定义，供占位图与预览复用。
enum WidgetSnapshotFactory {
    static let sample = WidgetDataProvider.WidgetSnapshot(
        summary: Summary(
            totalRequests: 482,
            successRequests: 470,
            errorRequests: 12,
            promptTokens: 1_500_000,
            completionTokens: 800_000,
            totalTokens: 2_300_000,
            cachedTokens: 500_000,
            cacheHitRate: 0.96,
            avgDurationMS: 320,
            nativeRatio: 0.85
        ),
        costToday: CostForecast(
            period: "today",
            spentUSD: 1.23,
            projectedUSD: 3.45,
            burnPerHourUSD: 0.15,
            dailyAvgUSD: 2.50,
            basis: "run_rate",
            confidence: "medium",
            reason: nil,
            budgetUSD: 0,
            projectedExceededDate: nil
        ),
        modelTop: [
            GroupStat(name: "gpt-4o", requests: 200, totalTokens: 1_000_000, cachedTokens: 300_000, cacheRate: 0.30, avgMS: 350),
            GroupStat(name: "claude-3-sonnet", requests: 150, totalTokens: 800_000, cachedTokens: 200_000, cacheRate: 0.25, avgMS: 280),
            GroupStat(name: "deepseek-chat", requests: 132, totalTokens: 500_000, cachedTokens: 100_000, cacheRate: 0.20, avgMS: 200),
        ],
        channelBalances: [],
        trend: WidgetSnapshotFactory.sampleTrend,
        sourceName: "XTokenHub",
        billingCurrency: "CNY",
        billingRate: 7.2,
        timestamp: Date(),
        source: .cache
    )

    /// 24 小时示例趋势（占位图用）。
    static let sampleTrend: [TrendPoint] = (0..<24).map { hour in
        TrendPoint(
            date: "",
            ts: Int64(Date().timeIntervalSince1970) - Int64((23 - hour) * 3600),
            requests: 40,
            totalTokens: hour >= 9 && hour <= 18 ? 4_200 : 1_600,
            errorRequests: 0
        )
    }
}
