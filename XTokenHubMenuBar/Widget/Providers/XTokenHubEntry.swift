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
    /// 设置里勾选可显示余额的渠道 id(nil = 未设置:全部显示;空 = 都不显示)。
    var balanceChannelIDs: [Int64]? { snapshot.balanceChannelIDs }
    var peakPrices: [ModelPrice] { snapshot.peakPrices }
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

    // MARK: - 余额展示（设置页勾选的渠道才会出现）

    /// 小组件余额的展示序列，按 `channel_id` 稳定排序；三档尺寸各取所需（Small 1 个、Medium/Large 最多 3 个）。
    ///
    /// - **未设置**（`balanceChannelIDs == nil`）：全部渠道里只留真的查到余额的 —— 与主 App 口径一致；
    /// - **勾选了渠道**：只有勾选的会出现，**没勾的一律不显示**；
    ///   勾选的渠道暂时查不到余额（不支持/查询失败）仍保留（名字 + `—`），
    ///   否则用户勾了却看不到，会以为没生效 —— 它们排在有余额的之后，不挤占真实金额的位置；
    /// - **一个都没勾**：序列为空，Small 回退「缓存命中」，Medium/Large 不出现余额区。
    ///
    /// `channelBalances` 来自字典，顺序不稳定，不排序会让每次刷新换一个渠道打头。
    var balanceCandidates: [ChannelBalance] {
        let ordered = channelBalances.sorted { $0.channelID < $1.channelID }
        guard let ids = balanceChannelIDs else {
            return ordered.filter { $0.balance != nil }
        }
        let selected = Set(ids)
        let picked = ordered.filter { selected.contains($0.channelID) }
        return picked.filter { $0.balance != nil } + picked.filter { $0.balance == nil }
    }

    /// 单栏展示（Small / Medium 取第一个）用的主余额。
    var featuredBalance: ChannelBalance? { balanceCandidates.first }

    /// 余额单行的分组：每行 `perRow` 个、最多 `maxRows` 行，返回（行列表，没显示的个数）。
    ///
    /// 行宽是硬约束：326pt 单行只放得下 **2 个**长名渠道 —— 3 个的理想宽实测 409pt，
    /// 排在第三个的 chip 会被挤到只剩几个像素，等于看不见（macOS 上这是静默裁切，
    /// 高度断言发现不了，见 `WidgetLayoutTests.balanceRowFitsSingleLineBudget`）。
    func balanceRows(perRow: Int = 2, maxRows: Int = 2) -> (rows: [[ChannelBalance]], overflow: Int) {
        let visible = Array(balanceCandidates.prefix(perRow * maxRows))
        let rows = stride(from: 0, to: visible.count, by: perRow).map {
            Array(visible[$0..<min($0 + perRow, visible.count)])
        }
        return (rows, balanceCandidates.count - visible.count)
    }

    // MARK: - 占位数据（Widget Gallery / 预览）

    static func placeholder() -> XTokenHubEntry {
        XTokenHubEntry(date: Date(), snapshot: WidgetSnapshotFactory.sample)
    }

    static func noData() -> XTokenHubEntry {
        XTokenHubEntry(date: Date(), snapshot: .empty)
    }
}

// MARK: - 峰谷主角

extension XTokenHubEntry {
    /// 峰谷主角：距下次切换最近的模型（多个模型时取最可操作的那个，与 Web 卡片一致）。
    struct PeakLead {
        let price: ModelPrice
        let rule: PeakWindow
        let period: PricePeriod
        let next: (at: Date, to: PricePeriod)?
        /// 判定用的节假日年历（`!H` / `W` 记号），费率行等复用同一口径。
        let calendar: HolidayCalendar?
    }

    /// 载荷携带的节假日年历（未来 60 天清单）；旧版主 App 未带时为 nil，
    /// `!H` / `W` 记号退化为按星期判定（与后端无年历口径一致）。
    var holidayCalendar: HolidayCalendar? {
        snapshot.holidays.map { HolidayCalendar(items: $0) }
    }

    /// 判定一律以传入时刻为准（通常是 entry.date：时间线在切换点有条目，到点重算即翻状态）。
    func peakLead(at date: Date) -> PeakLead? {
        var best: PeakLead?
        for price in snapshot.peakPrices {
            guard let rule = price.peakRule else { continue }
            let candidate = PeakLead(
                price: price,
                rule: rule,
                period: rule.period(at: date, calendar: holidayCalendar),
                next: rule.nextSwitch(after: date, calendar: holidayCalendar),
                calendar: holidayCalendar
            )
            guard let current = best else {
                best = candidate
                continue
            }
            // 无切换点（规则覆盖全周、恒为高峰）的模型排在有切换点的之后
            switch (candidate.next, current.next) {
            case (nil, _):
                continue
            case (_?, nil):
                best = candidate
            case (let c?, let b?) where c.at < b.at:
                best = candidate
            default:
                break
            }
        }
        return best
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
        channelBalances: [
            ChannelBalance(
                channelID: 1,
                channelName: "DeepSeek-官方",
                provider: "deepseek",
                supported: true,
                ok: true,
                balance: BalanceInfo(
                    provider: "deepseek",
                    isAvailable: true,
                    currency: "CNY",
                    total: 1234.56,
                    granted: 12.34,
                    toppedUp: 1000,
                    fetchedAt: ""
                ),
                error: nil,
                fetchedAt: nil
            ),
            // 不支持余额查询的渠道：自动模式下不该出现在小组件里
            ChannelBalance(
                channelID: 2,
                channelName: "OpenAI-转发",
                provider: "openai",
                supported: false,
                ok: false,
                balance: nil,
                error: "unsupported",
                fetchedAt: nil
            ),
            ChannelBalance(
                channelID: 3,
                channelName: "Claude-转发",
                provider: "anthropic",
                supported: true,
                ok: true,
                balance: BalanceInfo(
                    provider: "anthropic",
                    isAvailable: true,
                    currency: "USD",
                    total: 42.1,
                    granted: 0,
                    toppedUp: 50,
                    fetchedAt: ""
                ),
                error: nil,
                fetchedAt: nil
            ),
        ],
        peakPrices: [
            ModelPrice(
                id: 1,
                model: "deepseek-flash",
                currency: "CNY",
                inputCostPerToken: 2.0 / 1_000_000,
                outputCostPerToken: 8.0 / 1_000_000,
                peakWindow: "1-5,!H;09:00-12:00,14:00-18:00",
                offPeakInputCostPerToken: 1.0 / 1_000_000,
                offPeakOutputCostPerToken: 4.0 / 1_000_000
            )
        ],
        // 年历示例（2026 国庆）：预览里能看到 !H 规则「放假全天空闲」的效果。
        holidays: [
            HolidayDateItem(date: "2026-10-01", kind: "holiday"),
            HolidayDateItem(date: "2026-10-02", kind: "holiday"),
            HolidayDateItem(date: "2026-10-03", kind: "holiday"),
            HolidayDateItem(date: "2026-10-04", kind: "holiday"),
            HolidayDateItem(date: "2026-10-05", kind: "holiday"),
            HolidayDateItem(date: "2026-10-06", kind: "holiday"),
            HolidayDateItem(date: "2026-10-07", kind: "holiday"),
            HolidayDateItem(date: "2026-10-10", kind: "workday"),
        ],
        trend: WidgetSnapshotFactory.sampleTrend,
        sourceName: "XTokenHub",
        billingCurrency: "CNY",
        billingRate: 7.2,
        timestamp: Date(),
        source: .cache
    )

    /// 24 小时示例趋势（占位图用）。
    /// 逐项拆开赋值：写成单句 `(0..<24).map { TrendPoint(...) }` 会让 Xcode 26 的类型检查器
    /// 判定「无法在合理时间内完成」（CI 实测），显式标注每步类型即可。
    static let sampleTrend: [TrendPoint] = {
        let now = Int64(Date().timeIntervalSince1970)
        var points: [TrendPoint] = []
        points.reserveCapacity(24)
        for hour in 0..<24 {
            let ts: Int64 = now - Int64(23 - hour) * 3600
            let tokens: Int64 = (9...18).contains(hour) ? 4_200 : 1_600
            points.append(TrendPoint(
                date: "",
                ts: ts,
                requests: 40,
                totalTokens: tokens,
                errorRequests: 0
            ))
        }
        return points
    }()
}
