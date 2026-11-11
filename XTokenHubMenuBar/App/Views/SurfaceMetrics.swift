import SwiftUI

/// 一块展示面(菜单栏标签 / 悬浮卡片)的原始数值快照。
///
/// 视图只负责从 Store 拼快照,排序、占位符、单位拆分都在 `SurfaceMetrics` 的纯函数里,
/// 这样菜单栏与浮窗口径一致,也能脱离 Store 做单测。
struct SurfaceMetricSnapshot: Equatable {
    var totalRequests: Int64?
    var todayTokens: Int64 = 0
    var cacheHitRate: Double?
    var costTodayUSD: Double?
    var tokensPerSec: Double = 0
    /// 峰谷状态:取最近切换的模型;没有任何配置时段价的模型时为 nil。
    var peak: PricePeriod?
    var displayCurrency: String = "USD"
    var usdRate: Double = 0
    var isOnline: Bool = false
    var sourceName: String = ""
    var activeStreams: Int = 0

    var moneyOptions: MoneyFormatter.Options {
        MoneyFormatter.Options(currency: displayCurrency, rate: usdRate)
    }
}

extension SurfaceMetricSnapshot {
    /// 从 Store 取当前值。`now` 可注入,便于测峰谷判定。
    @MainActor
    init(store: HubStore, settings: AppSettings, now: Date = Date()) {
        let holidays = store.holidayCalendar
        var best: (switchAt: Date, period: PricePeriod)?
        for price in store.peakPrices {
            guard let rule = price.peakRule else { continue }
            let candidate = (
                switchAt: rule.nextSwitch(after: now, calendar: holidays)?.at ?? .distantFuture,
                period: rule.period(at: now, calendar: holidays)
            )
            if best == nil || candidate.switchAt < best!.switchAt {
                best = candidate
            }
        }

        self.init(
            totalRequests: store.summary?.totalRequests,
            todayTokens: store.todayTokens,
            cacheHitRate: store.summary?.cacheHitRate,
            costTodayUSD: store.costToday?.spentUSD,
            tokensPerSec: store.tokensPerSec,
            peak: best?.period,
            displayCurrency: store.billing?.displayCurrency ?? "USD",
            usdRate: store.billing?.usdRate ?? 0,
            isOnline: store.connection.isOnline,
            sourceName: settings.selectedSource.name,
            activeStreams: store.activeStreams
        )
    }
}

/// 菜单栏标签与悬浮卡片共用的指标格式化。
///
/// 顺序固定:请求数 → Token → 命中率 → 花费 → 速度 → 峰谷;
/// 浮窗固定自带状态图标,`.icon` 只影响菜单栏。
enum SurfaceMetrics {
    static func parts(
        _ snapshot: SurfaceMetricSnapshot,
        metrics: Set<AppSettings.MenuBarMetric>
    ) -> [String] {
        var parts: [String] = []
        if metrics.contains(.requests) {
            parts.append(String(snapshot.totalRequests ?? 0))
        }
        if metrics.contains(.tokens) {
            parts.append(TokenFormatter.compact(snapshot.todayTokens))
        }
        if metrics.contains(.cacheHit) {
            parts.append(cacheHitText(snapshot))
        }
        if metrics.contains(.costToday) {
            parts.append(costTodayText(snapshot))
        }
        if metrics.contains(.throughput) {
            parts.append("\(TokenFormatter.compactSpeed(snapshot.tokensPerSec))/s")
        }
        if metrics.contains(.peakStatus), let peak = snapshot.peak {
            parts.append(peak == .peak ? "峰" : "谷")
        }
        return parts
    }

    /// 悬浮卡片的统计单元(两列网格,`StatGrid` 直接用)。
    static func items(
        _ snapshot: SurfaceMetricSnapshot,
        metrics: Set<AppSettings.MenuBarMetric>
    ) -> [StatItem] {
        var items: [StatItem] = []

        if metrics.contains(.requests) {
            items.append(StatItem(
                title: "请求 · 今日",
                value: snapshot.totalRequests.map(String.init) ?? "—",
                icon: "number",
                help: "当天请求总数"
            ))
        }

        if metrics.contains(.tokens) {
            let parts = TokenFormatter.splitCompact(snapshot.todayTokens)
            items.append(StatItem(
                title: "Token · 今日",
                value: parts.number,
                unit: parts.unit,
                icon: "paperplane.fill",
                help: "今天 · prompt + completion"
            ))
        }

        if metrics.contains(.cacheHit) {
            items.append(StatItem(
                title: "缓存命中",
                value: snapshot.cacheHitRate.map { String(format: "%.1f%%", $0 * 100) } ?? "—",
                icon: "memorychip",
                help: "cached / prompt"
            ))
        }

        if metrics.contains(.costToday) {
            items.append(StatItem(
                title: "今日花费",
                value: snapshot.costTodayUSD.map {
                    MoneyFormatter.format(usd: $0, options: snapshot.moneyOptions)
                } ?? "—",
                icon: "dollarsign.circle",
                help: "按设置页的展示币种与汇率"
            ))
        }

        if metrics.contains(.throughput) {
            items.append(StatItem(
                title: "实时速度",
                value: TokenFormatter.compactSpeed(snapshot.tokensPerSec),
                unit: "/s",
                icon: "bolt.fill",
                tint: .orange,
                help: "WebSocket 实时输出速度"
            ))
        }

        if metrics.contains(.peakStatus) {
            let isOffPeak = snapshot.peak == .offPeak
            items.append(StatItem(
                title: "峰谷",
                value: snapshot.peak.map { $0 == .peak ? "峰" : "谷" } ?? "—",
                icon: isOffPeak ? "moon.zzz.fill" : "sun.max.fill",
                tint: snapshot.peak == nil ? .secondary : (isOffPeak ? .green : .orange),
                help: "取最近切换的模型;没有配置时段价的模型时显示 —"
            ))
        }

        return items
    }

    static func cacheHitText(_ snapshot: SurfaceMetricSnapshot) -> String {
        guard let rate = snapshot.cacheHitRate else { return "—" }
        return String(format: "%.1f%%", rate * 100)
    }

    /// 今日花费(尚未加载时显示占位,避免显示成 $0 误导)。
    static func costTodayText(_ snapshot: SurfaceMetricSnapshot) -> String {
        guard let cost = snapshot.costTodayUSD else { return "—" }
        return MoneyFormatter.format(usd: cost, options: snapshot.moneyOptions)
    }

    static func symbol(_ snapshot: SurfaceMetricSnapshot) -> String {
        snapshot.isOnline ? "chart.line.uptrend.xyaxis" : "wifi.slash"
    }
}
