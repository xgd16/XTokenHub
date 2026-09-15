import AppKit
import SwiftUI
import Testing

/// 小组件离屏排版预算验证。
///
/// macOS 上内容溢出容器是**静默裁切**（不报错、不滚动），所以三档尺寸必须逐场景量出来：
/// 这里按各档可用内容区宽度量理想高度，超出预算即失败。
/// 场景覆盖：无峰谷 / 单模型高峰 / 单模型空闲 / 多模型（峰谷行新增后的主要风险点）。
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
