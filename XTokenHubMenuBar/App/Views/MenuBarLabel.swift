import SwiftUI

/// 菜单栏标签:按设置的指标组合渲染,格式如 `482 · 3214.45万 · 96.0% · 12/s`。
/// 数字只用「数字 + 万/亿」单位,不带 tok 后缀;全部取消勾选时回退为图标。
/// 数值读取发生在本视图 body 中,@Observable 依赖跟踪会驱动标签自动刷新。
struct MenuBarLabel: View {
    let store: HubStore
    let settings: AppSettings

    var body: some View {
        let parts = textParts
        if parts.isEmpty {
            Image(systemName: symbol)
        } else if settings.menuBarMetrics.contains(.icon) {
            Label(parts.joined(separator: separator), systemImage: symbol)
        } else {
            Text(parts.joined(separator: separator))
        }
    }

    private var separator: String { " · " }

    private var textParts: [String] {
        var parts: [String] = []
        let metrics = settings.menuBarMetrics
        if metrics.contains(.requests) {
            parts.append(String(store.summary?.totalRequests ?? 0))
        }
        if metrics.contains(.tokens) {
            parts.append(TokenFormatter.compact(store.todayTokens))
        }
        if metrics.contains(.cacheHit) {
            parts.append(cacheHitText)
        }
        if metrics.contains(.costToday) {
            parts.append(costTodayText)
        }
        if metrics.contains(.throughput) {
            parts.append("\(TokenFormatter.compactSpeed(store.tokensPerSec))/s")
        }
        return parts
    }

    private var cacheHitText: String {
        guard let rate = store.summary?.cacheHitRate else { return "—" }
        return String(format: "%.1f%%", rate * 100)
    }

    /// 今日花费(尚未加载时显示占位,避免显示成 $0 误导)。
    private var costTodayText: String {
        guard let cost = store.costToday else { return "—" }
        return MoneyFormatter.format(usd: cost.spentUSD, options: moneyOptions)
    }

    private var moneyOptions: MoneyFormatter.Options {
        MoneyFormatter.Options(
            currency: store.billing?.displayCurrency ?? "USD",
            rate: store.billing?.usdRate ?? 0
        )
    }

    private var symbol: String {
        store.connection.isOnline ? "chart.line.uptrend.xyaxis" : "wifi.slash"
    }
}
