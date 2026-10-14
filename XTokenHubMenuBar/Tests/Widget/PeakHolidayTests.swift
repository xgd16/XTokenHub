import Foundation
import Testing

/// 小组件峰谷主角的节假日场景：年历随 WidgetBridgePayload / 本地缓存带到 Widget，
/// 离线也能判对 `!H` / `W`（镜像 Tests/PeakWindowTests 的节假日用例，补 lead/倒计时口径）。
struct PeakHolidayLeadTests {
    /// 2026 国庆年历样本：10-01(周四)~10-07 放假，10-10(周六)调休补班。
    private static let holidayItems: [HolidayDateItem] = [
        HolidayDateItem(date: "2026-10-01", kind: "holiday"),
        HolidayDateItem(date: "2026-10-02", kind: "holiday"),
        HolidayDateItem(date: "2026-10-03", kind: "holiday"),
        HolidayDateItem(date: "2026-10-04", kind: "holiday"),
        HolidayDateItem(date: "2026-10-05", kind: "holiday"),
        HolidayDateItem(date: "2026-10-06", kind: "holiday"),
        HolidayDateItem(date: "2026-10-07", kind: "holiday"),
        HolidayDateItem(date: "2026-10-10", kind: "workday"),
    ]

    private static func date(_ year: Int, _ month: Int, _ day: Int, _ hour: Int, _ minute: Int) -> Date {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = PeakWindow.beijingTimeZone
        return cal.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute))!
    }

    /// 单模型快照：规则与年历按用例给定（年历经 snapshot.holidays 通道进入判定）。
    private static func entry(at date: Date, rule: String, holidays: [HolidayDateItem]?) -> XTokenHubEntry {
        var snapshot = WidgetSnapshotFactory.sample
        snapshot.peakPrices = [
            ModelPrice(
                id: 1,
                model: "deepseek-flash",
                currency: "CNY",
                inputCostPerToken: 2.0 / 1_000_000,
                outputCostPerToken: 8.0 / 1_000_000,
                peakWindow: rule,
                offPeakInputCostPerToken: 1.0 / 1_000_000,
                offPeakOutputCostPerToken: 4.0 / 1_000_000
            ),
        ]
        snapshot.holidays = holidays
        return XTokenHubEntry(date: date, snapshot: snapshot)
    }

    /// 假期里主角是谷，倒计时指向节后首个工作日的转高峰。
    @Test func leadCountsDownToPostHolidaySwitchOnHoliday() throws {
        let at = Self.date(2026, 10, 2, 10, 0) // 周五，国庆假期内
        let entry = Self.entry(at: at, rule: "1-5,!H;09:00-12:00,14:00-18:00", holidays: Self.holidayItems)
        let lead = try #require(entry.peakLead(at: at))
        #expect(lead.period == .offPeak)
        let next = try #require(lead.next)
        #expect(next.at == Self.date(2026, 10, 8, 9, 0))
        #expect(next.to == .peak)
        #expect(peakCountdown(from: at, to: next.at) == "5d23h")
    }

    /// 无年历（旧版主 App 未带、缓存也没有）退化为按星期：同一时刻按普通周五判高峰。
    @Test func leadFallsBackToWeekdayWithoutCalendar() throws {
        let at = Self.date(2026, 10, 2, 10, 0)
        let entry = Self.entry(at: at, rule: "1-5,!H;09:00-12:00,14:00-18:00", holidays: nil)
        let lead = try #require(entry.peakLead(at: at))
        #expect(lead.period == .peak)
    }

    /// 补班周六与工作日一致：白天高峰，12:00 切入空闲（倒计时 2h0m）。
    @Test func makeupWorkdayLeadBehavesLikeWorkday() throws {
        let at = Self.date(2026, 10, 10, 10, 0) // 周六，调休补班
        let entry = Self.entry(at: at, rule: "1-5,!H,W;09:00-12:00,14:00-18:00", holidays: Self.holidayItems)
        let lead = try #require(entry.peakLead(at: at))
        #expect(lead.period == .peak)
        let next = try #require(lead.next)
        #expect(next.at == Self.date(2026, 10, 10, 12, 0))
        #expect(next.to == .offPeak)
        #expect(peakCountdown(from: at, to: next.at) == "2h0m")
    }
}
