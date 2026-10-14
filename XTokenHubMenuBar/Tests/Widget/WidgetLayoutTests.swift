import AppKit
import SwiftUI
import Testing

/// 小组件离屏排版预算验证。
///
/// macOS 上内容溢出容器是**静默裁切**（不报错、不滚动），所以三档尺寸必须逐场景量出来：
/// 这里按各档可用内容区宽度量理想高度，超出预算即失败。
/// 场景覆盖：无峰谷 / 单模型高峰 / 单模型空闲 / 多模型（峰谷行新增后的主要风险点）；
/// 示例快照自带渠道余额，因此每档也同时覆盖「余额块」这条新增内容。
@MainActor
struct WidgetLayoutTests {
    /// 各档可用内容区（已扣掉系统容器边距；数值取自各视图顶部的垂直预算注释）。
    private static let smallSize = CGSize(width: 138, height: 138)
    private static let mediumSize = CGSize(width: 326, height: 138)
    private static let largeSize = CGSize(width: 326, height: 326)

    private struct Scenario {
        let name: String
        let date: Date
        let prices: [ModelPrice]
    }

    /// 2026-01-05 是周一：10:00 高峰，12:30 午休空闲。
    private static func scenarios() -> [Scenario] {
        let peak = date(2026, 1, 5, 10, 0)
        let offPeak = date(2026, 1, 5, 12, 30)
        let single = WidgetSnapshotFactory.sample.peakPrices
        let multi = single + [
            ModelPrice(
                id: 2,
                model: "deepseek-reasoner",
                currency: "USD",
                inputCostPerToken: 0.55 / 1_000_000,
                outputCostPerToken: 2.19 / 1_000_000,
                peakWindow: "1-5;09:00-12:00,14:00-18:00",
                offPeakInputCostPerToken: 0.275 / 1_000_000,
                offPeakOutputCostPerToken: 1.095 / 1_000_000
            ),
            ModelPrice(
                id: 3,
                model: "gpt-4o",
                currency: "USD",
                inputCostPerToken: 2.5 / 1_000_000,
                outputCostPerToken: 10 / 1_000_000,
                peakWindow: "6-7;10:00-11:00",
                offPeakInputCostPerToken: 1.25 / 1_000_000,
                offPeakOutputCostPerToken: 5 / 1_000_000
            ),
        ]
        return [
            Scenario(name: "无峰谷", date: peak, prices: []),
            Scenario(name: "单模型·高峰", date: peak, prices: single),
            Scenario(name: "单模型·空闲", date: offPeak, prices: single),
            Scenario(name: "多模型·高峰", date: peak, prices: multi),
        ]
    }

    private static func date(_ year: Int, _ month: Int, _ day: Int, _ hour: Int, _ minute: Int) -> Date {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = PeakWindow.beijingTimeZone
        return cal.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute))!
    }

    private func entry(_ scenario: Scenario) -> XTokenHubEntry {
        var snapshot = WidgetSnapshotFactory.sample
        snapshot.peakPrices = scenario.prices
        return XTokenHubEntry(date: scenario.date, snapshot: snapshot)
    }

    /// 给定宽度下的理想高度（内容放不下时会大于预算，即静默裁切的判据）。
    /// macOS 没有 UIView.sizeThatFits：先按目标宽度定框，再取 NSHostingView.fittingSize。
    private func idealHeight(_ view: some View, width: CGFloat) -> CGFloat {
        let host = NSHostingView(rootView: view)
        host.setFrameSize(NSSize(width: width, height: 0))
        host.layoutSubtreeIfNeeded()
        return host.fittingSize.height
    }

    /// 不给宽度时的理想宽度：内容横向放不下的判据（放不下就会被静默裁掉）。
    private func idealWidth(_ view: some View, height: CGFloat) -> CGFloat {
        let host = NSHostingView(rootView: view)
        host.setFrameSize(NSSize(width: 0, height: height))
        host.layoutSubtreeIfNeeded()
        return host.fittingSize.width
    }

    @Test func threeSizesFitAcrossScenarios() {
        for scenario in Self.scenarios() {
            let entry = entry(scenario)

            let small = idealHeight(SmallWidgetView(entry: entry), width: Self.smallSize.width)
            #expect(small <= Self.smallSize.height, "Small · \(scenario.name): 内容 \(small)pt 超出 \(Self.smallSize.height)pt")

            let medium = idealHeight(MediumWidgetView(entry: entry), width: Self.mediumSize.width)
            #expect(medium <= Self.mediumSize.height, "Medium · \(scenario.name): 内容 \(medium)pt 超出 \(Self.mediumSize.height)pt")

            let large = idealHeight(LargeWidgetView(entry: entry), width: Self.largeSize.width)
            #expect(large <= Self.largeSize.height, "Large · \(scenario.name): 内容 \(large)pt 超出 \(Self.largeSize.height)pt")
        }
    }

    /// 余额块与「缓存命中」同构:Small 有无余额都不改变高度,否则会顶出 138pt 边界。
    @Test func balanceKeepsSmallHeight() {
        let scenario = Self.scenarios()[1] // 有峰谷的样本 = 最挤的一档(余额来自示例快照)
        let withBalance = idealHeight(SmallWidgetView(entry: entry(scenario)), width: Self.smallSize.width)

        var snapshot = WidgetSnapshotFactory.sample
        snapshot.channelBalances = []
        let without = XTokenHubEntry(date: scenario.date, snapshot: snapshot)
        let withoutBalance = idealHeight(SmallWidgetView(entry: without), width: Self.smallSize.width)

        #expect(withBalance <= Self.smallSize.height, "带余额 Small 高度 \(withBalance)pt 超出 \(Self.smallSize.height)pt")
        #expect(withoutBalance <= Self.smallSize.height, "回退缓存命中后仍应放得下:\(withoutBalance)pt")
        #expect(withBalance == withoutBalance, "余额应与缓存命中等高(有 \(withBalance)pt / 无 \(withoutBalance)pt)")
    }

    /// 9 个渠道全部勾选（含长名与查不到余额的），最挤的一档。
    private static func crowdedEntry() -> XTokenHubEntry {
        var snapshot = WidgetSnapshotFactory.sample
        var extra: [ChannelBalance] = []
        for id: Int64 in 10..<16 {
            extra.append(ChannelBalance(
                channelID: id,
                channelName: "渠道-\(id)",
                provider: "openai",
                supported: true,
                ok: true,
                balance: BalanceInfo(
                    provider: "openai",
                    isAvailable: true,
                    currency: "USD",
                    total: Double(id),
                    granted: 0,
                    toppedUp: 10,
                    fetchedAt: ""
                ),
                error: nil,
                fetchedAt: nil
            ))
        }
        snapshot.channelBalances += extra
        snapshot.balanceChannelIDs = snapshot.channelBalances.map(\.channelID) // 全部勾选
        return XTokenHubEntry(date: scenarios()[1].date, snapshot: snapshot)
    }

    /// 勾选很多渠道时:Small 1 个、Medium 最多 3 行、Large 每行 2 个最多两行,三档都不许溢出。
    @Test func manyCheckedBalancesFit() {
        let entry = Self.crowdedEntry()
        // 示例快照 3 个(其中 1 个无余额)+ 新增 6 个 = 9 个全勾上;无余额的排在最后
        #expect(entry.balanceCandidates.count == 9)
        #expect(entry.balanceCandidates.last?.channelID == 2)

        let small = idealHeight(SmallWidgetView(entry: entry), width: Self.smallSize.width)
        #expect(small <= Self.smallSize.height, "Small 多余额: \(small)pt 超出 \(Self.smallSize.height)pt")

        let medium = idealHeight(MediumWidgetView(entry: entry), width: Self.mediumSize.width)
        #expect(medium <= Self.mediumSize.height, "Medium 多余额: \(medium)pt 超出 \(Self.mediumSize.height)pt")

        let large = idealHeight(LargeWidgetView(entry: entry), width: Self.largeSize.width)
        #expect(large <= Self.largeSize.height, "Large 多余额: \(large)pt 超出 \(Self.largeSize.height)pt")
    }

    /// 余额分组规则:每行 2 个、最多 2 行,溢出数正确 —— Small/Medium/Large 各取所需。
    @Test func balanceRowsChunkByTwo() {
        let entry = Self.crowdedEntry()
        let (rows, overflow) = entry.balanceRows()

        #expect(rows.count <= 2)
        #expect(rows.allSatisfy { $0.count <= 2 })
        #expect(overflow == entry.balanceCandidates.count - rows.reduce(0) { $0 + $1.count })
        #expect(rows.flatMap { $0.map(\.channelID) } == [1, 3, 10, 11]) // 按 id 取前 4 个
        #expect(overflow == 5)

        // 只勾 2 个:一行装下、无溢出
        var single = WidgetSnapshotFactory.sample
        single.balanceChannelIDs = [1, 3]
        let two = XTokenHubEntry(date: entry.date, snapshot: single).balanceRows()
        #expect(two.rows.count == 1)
        #expect(two.overflow == 0)
    }

    /// 余额单行的**横向**预算:326pt 单行只放得下 2 个长名渠道。
    ///
    /// 反例(3 个)必须测出超预算 —— 否则这条断言就是空转,而线上表现是
    /// 第三个 chip 被挤到几个像素宽、等于看不见(高度断言完全发现不了)。
    @Test func balanceRowFitsSingleLineBudget() {
        let entry = Self.crowdedEntry()
        let (rows, overflow) = entry.balanceRows()
        #expect(rows.count == 2 && overflow == 5)

        for (index, balances) in rows.enumerated() {
            let row = ChannelBalanceRow(
                balances: balances,
                extraCount: index == rows.count - 1 ? overflow : 0
            )
            let width = idealWidth(row, height: 13)
            #expect(width <= Self.largeSize.width, "余额行 \(index) 理想宽 \(width)pt 超出 \(Self.largeSize.width)pt,右侧会被静默裁掉")
        }

        // 反例:同样的渠道硬塞进一行,必须超出预算(证明上面的断言测得出问题)
        let cramped = ChannelBalanceRow(balances: Array(entry.balanceCandidates.prefix(3)), extraCount: 0)
        let crampedWidth = idealWidth(cramped, height: 13)
        #expect(crampedWidth > Self.largeSize.width, "3 个 chip 理想宽 \(crampedWidth)pt 竟然 ≤ \(Self.largeSize.width)pt,预算断言失效")
    }

    /// 勾选集合为空(一个渠道都不显示):三档都退回无余额布局,同样不溢出。
    @Test func noCheckedBalanceFits() {
        var snapshot = WidgetSnapshotFactory.sample
        snapshot.balanceChannelIDs = []
        let entry = XTokenHubEntry(date: Self.scenarios()[1].date, snapshot: snapshot)
        #expect(entry.featuredBalance == nil)

        #expect(idealHeight(SmallWidgetView(entry: entry), width: Self.smallSize.width) <= Self.smallSize.height)
        #expect(idealHeight(MediumWidgetView(entry: entry), width: Self.mediumSize.width) <= Self.mediumSize.height)
        #expect(idealHeight(LargeWidgetView(entry: entry), width: Self.largeSize.width) <= Self.largeSize.height)
    }

    /// 峰谷行必须真的参与排版（防止「加了视图但被静默裁掉」这种假通过）。
    @Test func peakRowActuallyOccupiesSpace() {
        let scenarios = Self.scenarios()
        let withoutPeak = idealHeight(SmallWidgetView(entry: entry(scenarios[0])), width: Self.smallSize.width)
        let withPeak = idealHeight(SmallWidgetView(entry: entry(scenarios[1])), width: Self.smallSize.width)
        #expect(withPeak > withoutPeak, "Small 加入峰谷行后内容高度应增加（无峰谷 \(withoutPeak)pt / 有峰谷 \(withPeak)pt）")
    }

    /// 峰谷行在 Small/Medium 宽度下不折行（折行会挤掉趋势图）。
    @Test func peakRowStaysSingleLine() {
        for scenario in Self.scenarios() where !scenario.prices.isEmpty {
            let entry = entry(scenario)
            let row = PeakStatusRow(lead: entry.peakLead(at: entry.date)!, date: entry.date)
            let height = idealHeight(row, width: Self.smallSize.width)
            #expect(height <= 14, "峰谷行 \(scenario.name) 在 Small 宽度下高度 \(height)pt,疑似折行")
        }
    }
}
