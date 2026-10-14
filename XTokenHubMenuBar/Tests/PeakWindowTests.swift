import Foundation
import Testing
@testable import XTokenHubMenuBar

/// 构造北京时间,便于表达时段断言(与后端测试的 bj 助手同口径)。
private func bj(_ year: Int, _ month: Int, _ day: Int, _ hour: Int, _ minute: Int) -> Date {
    var cal = Calendar(identifier: .gregorian)
    cal.timeZone = PeakWindow.beijingTimeZone
    return cal.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute))!
}

/// DeepSeek 官方错峰规则:工作日 9:00-12:00、14:00-18:00 为高峰。
private let deepseekWindow = "1-5;09:00-12:00,14:00-18:00"

/// 示例价目行:CNY 高峰 ¥2/¥8、空闲 ¥1/¥4(省 50%),未配置缓存费率。
private func makePrice() -> ModelPrice {
    ModelPrice(
        id: 1,
        model: "deepseek-flash",
        currency: "CNY",
        inputCostPerToken: 2 / 1_000_000,
        outputCostPerToken: 8 / 1_000_000,
        peakWindow: deepseekWindow,
        offPeakInputCostPerToken: 1 / 1_000_000,
        offPeakOutputCostPerToken: 4 / 1_000_000
    )
}

// MARK: - 规则解析(镜像 internal/tests/model/peak_window_test.go)

struct PeakWindowParsingTests {
    @Test func parsesValidRules() {
        for rule in [
            deepseekWindow,
            "1;09:00-12:00",
            "1-7;00:00-23:59",
            "1,3,5;09:00-12:00,14:00-18:00", // 列表
            "6-7;10:00-11:00",               // 周末
            " 1-5 ; 09:00 - 12:00 ",         // 空白容忍
            "1;9:00-12:00",                  // 单位数小时
        ] {
            #expect(PeakWindow(rule) != nil, "应能解析: \(rule)")
        }
    }

    @Test func rejectsInvalidRules() {
        for rule in [
            "",                            // 空串 = 未启用(按 nil 处理)
            "1-5",                         // 缺时段段
            "1-5;",                        // 空时段段
            ";09:00-12:00",                // 空星期段
            "0;09:00-12:00",               // 星期越界(1..7)
            "8;09:00-12:00",               // 星期越界
            "5-1;09:00-12:00",             // 区间起点大于终点
            "1;12:00-09:00",               // 结束早于开始(不支持跨零点)
            "1;09:00-09:00",               // 零长区间
            "1;24:00-25:00",               // 小时越界
            "1;09:60-12:00",               // 分钟越界
            "1;0900-1200",                 // 缺冒号
            "1;09:00-12:00,",              // 尾随逗号产生空项
            "1-5;09:00-12:00;14:00-18:00", // 段数过多(分号应为两段)
        ] {
            #expect(PeakWindow(rule) == nil, "应拒绝: \(rule)")
        }
    }
}

// MARK: - 时段判定

struct PeakWindowJudgmentTests {
    @Test func judgesWorkdayBoundaries() throws {
        let window = try #require(PeakWindow(deepseekWindow))
        // 2026-01-05 是周一,01-03 是周六,01-04 是周日。
        let cases: [(name: String, at: Date, want: Bool)] = [
            ("工作日 上午高峰起点 09:00", bj(2026, 1, 5, 9, 0), true),
            ("工作日 上午高峰中段 10:30", bj(2026, 1, 5, 10, 30), true),
            ("工作日 上午高峰终点前 11:59", bj(2026, 1, 5, 11, 59), true),
            ("工作日 午休(12:00 已是空闲)", bj(2026, 1, 5, 12, 0), false),
            ("工作日 午休中段 13:00", bj(2026, 1, 5, 13, 0), false),
            ("工作日 下午高峰起点 14:00", bj(2026, 1, 5, 14, 0), true),
            ("工作日 下午高峰中段 16:00", bj(2026, 1, 5, 16, 0), true),
            ("工作日 下午高峰终点 18:00(不含)", bj(2026, 1, 5, 18, 0), false),
            ("工作日 晚间 20:00", bj(2026, 1, 5, 20, 0), false),
            ("工作日 凌晨 03:00", bj(2026, 1, 5, 3, 0), false),
            ("周五 上午高峰", bj(2026, 1, 9, 10, 0), true),
            ("周六 上午(周末非高峰)", bj(2026, 1, 3, 10, 0), false),
            ("周日 下午(周末非高峰)", bj(2026, 1, 4, 15, 0), false),
            ("周一 23:59", bj(2026, 1, 5, 23, 59), false),
        ]
        for c in cases {
            #expect(window.isPeak(at: c.at) == c.want, "\(c.name)")
            #expect(window.period(at: c.at) == (c.want ? .peak : .offPeak), "\(c.name) 的时段")
        }
    }

    /// 判定必须按北京时间,与入参时刻自身携带的时区表示无关。
    @Test func judgmentIsTimezoneIndependent() throws {
        let window = try #require(PeakWindow(deepseekWindow))
        // 北京 2026-01-05 10:00 == UTC 02:00,同一瞬间。
        let beijing = bj(2026, 1, 5, 10, 0)
        let utcSameInstant = try #require(ISO8601DateFormatter().date(from: "2026-01-05T02:00:00Z"))
        #expect(abs(utcSameInstant.timeIntervalSince(beijing)) < 0.5)
        #expect(window.isPeak(at: beijing))
        #expect(window.isPeak(at: utcSameInstant))
    }

    /// 跨日/多时段:夜间窗口不应在白天命中。
    @Test func nightSpansDoNotMatchDaytime() throws {
        let window = try #require(PeakWindow("1-7;00:00-06:00,22:00-23:59"))
        #expect(window.isPeak(at: bj(2026, 1, 5, 3, 0)))
        #expect(!window.isPeak(at: bj(2026, 1, 5, 12, 0)))
        #expect(window.isPeak(at: bj(2026, 1, 5, 23, 0)))
        #expect(!window.isPeak(at: bj(2026, 1, 5, 23, 59)))
    }
}

// MARK: - 下次切换

struct PeakSwitchTests {
    @Test func findsNextSwitchWithinDay() throws {
        let window = try #require(PeakWindow(deepseekWindow))
        // 上午高峰中段 → 12:00 切入空闲
        let next = try #require(window.nextSwitch(after: bj(2026, 1, 5, 10, 0)))
        #expect(next.at == bj(2026, 1, 5, 12, 0))
        #expect(next.to == .offPeak)
        #expect(peakCountdown(from: bj(2026, 1, 5, 10, 0), to: next.at) == "2h0m")
    }

    @Test func findsNextSwitchAfterLunch() throws {
        let window = try #require(PeakWindow(deepseekWindow))
        let next = try #require(window.nextSwitch(after: bj(2026, 1, 5, 12, 30)))
        #expect(next.at == bj(2026, 1, 5, 14, 0))
        #expect(next.to == .peak)
    }

    @Test func crossesNightToNextWorkday() throws {
        let window = try #require(PeakWindow(deepseekWindow))
        let next = try #require(window.nextSwitch(after: bj(2026, 1, 5, 20, 0)))
        #expect(next.at == bj(2026, 1, 6, 9, 0))
        #expect(next.to == .peak)
    }

    @Test func crossesWeekendToMonday() throws {
        let window = try #require(PeakWindow(deepseekWindow))
        let next = try #require(window.nextSwitch(after: bj(2026, 1, 9, 20, 0)))
        #expect(next.at == bj(2026, 1, 12, 9, 0))
        #expect(next.to == .peak)
    }

    /// 边界取「严格晚于」:正好落在切换点上时应算出下一次。
    @Test func boundaryIsStrictlyAfter() throws {
        let window = try #require(PeakWindow(deepseekWindow))
        let atBoundary = try #require(window.nextSwitch(after: bj(2026, 1, 5, 12, 0)))
        #expect(atBoundary.at == bj(2026, 1, 5, 14, 0))
        #expect(atBoundary.to == .peak)

        let beforeBoundary = try #require(window.nextSwitch(after: bj(2026, 1, 5, 11, 59)))
        #expect(beforeBoundary.at == bj(2026, 1, 5, 12, 0))
        #expect(beforeBoundary.to == .offPeak)
    }

    /// 近乎全天高峰的规则:23:59 结束(不含)仍构成一个切换点。
    @Test func nearAllDayRuleSwitchesAtDayEnd() throws {
        let window = try #require(PeakWindow("1-7;00:00-23:59"))
        #expect(window.period(at: bj(2026, 1, 5, 10, 0)) == .peak)
        let next = try #require(window.nextSwitch(after: bj(2026, 1, 5, 10, 0)))
        #expect(next.at == bj(2026, 1, 5, 23, 59))
        #expect(next.to == .offPeak)
    }
}

// MARK: - 倒计时文案

struct PeakCountdownTests {
    @Test func rendersCompactCountdown() {
        let now = bj(2026, 1, 5, 10, 0)
        #expect(peakCountdown(from: now, to: bj(2026, 1, 5, 11, 35)) == "1h35m")
        #expect(peakCountdown(from: now, to: bj(2026, 1, 5, 10, 47)) == "47m")
        #expect(peakCountdown(from: now, to: bj(2026, 1, 7, 13, 0)) == "2d3h")
        #expect(peakCountdown(from: now, to: bj(2026, 1, 5, 9, 0)) == "0m")
    }
}

// MARK: - 价目行解码与费率

struct ModelPriceTests {
    /// 后端真实形状:金额为单 token、CNY 行、含 off_peak 四项。
    @Test func decodesPeakRow() throws {
        let json = """
        {"id":9,"model":"deepseek-flash","currency":"CNY","input_cost_per_token":0.000002,
        "output_cost_per_token":0.000008,"cache_read_cost_per_token":0.0000002,
        "cache_write_cost_per_token":0,"peak_window":"1-5;09:00-12:00,14:00-18:00",
        "off_peak_input_cost_per_token":0.000001,"off_peak_output_cost_per_token":0.000004,
        "off_peak_cache_read_cost_per_token":0,"off_peak_cache_write_cost_per_token":0,
        "threshold_tokens":0,"source":"manual"}
        """
        let price = try JSONDecoder().decode(ModelPrice.self, from: Data(json.utf8))
        #expect(price.model == "deepseek-flash")
        #expect(price.displayCurrency == "CNY")
        #expect(price.peakRule != nil)
        #expect(price.offPeakDiscountPercent == 50)
    }

    /// 旧后端/同步行缺 peak_window 与 off_peak 键时按未配置处理,不抛错。
    @Test func toleratesRowsWithoutPeakFields() throws {
        let json = #"{"id":1,"model":"gpt-4o","input_cost_per_token":0.000005,"output_cost_per_token":0.000015}"#
        let price = try JSONDecoder().decode(ModelPrice.self, from: Data(json.utf8))
        #expect(price.displayCurrency == "USD")
        #expect(price.peakRule == nil)
        #expect(price.offPeakDiscountPercent == nil)
    }

    /// 空闲费率与缓存回退口径与后端 Rates 一致。
    /// 断言里的字面量写成显式 Double:#expect 宏展开会丢失上下文类型,否则会被当作整数除法。
    @Test func effectiveRatesFollowPeriodAndCacheFallback() {
        let price = makePrice()

        let peak = price.effectiveRates(at: bj(2026, 1, 5, 10, 0))
        #expect(peak.input == 2.0 / 1_000_000)
        #expect(peak.output == 8.0 / 1_000_000)
        // 未配置缓存费率 → 回退本次生效的输入费率
        #expect(peak.cacheRead == 2.0 / 1_000_000)
        #expect(peak.cacheWrite == 2.0 / 1_000_000)

        let off = price.effectiveRates(at: bj(2026, 1, 5, 12, 30))
        #expect(off.input == 1.0 / 1_000_000)
        #expect(off.output == 4.0 / 1_000_000)
        #expect(off.cacheRead == 1.0 / 1_000_000)
    }

    @Test func formatsPerMillionRates() {
        #expect(MoneyFormatter.formatPerMillion(perToken: 2 / 1_000_000, currency: "CNY") == "¥2")
        #expect(MoneyFormatter.formatPerMillion(perToken: 8 / 1_000_000, currency: "USD") == "$8")
        #expect(MoneyFormatter.formatPerMillion(perToken: 0.55 / 1_000_000, currency: "USD") == "$0.55")
        #expect(MoneyFormatter.formatPerMillion(perToken: 0, currency: "USD") == "—")
    }
}

// MARK: - 通知文案

struct PeakNotificationTests {
    @Test func offPeakContentCarriesRatesAndDiscount() {
        let text = PeakNotifier.notificationContent(price: makePrice(), switchingTo: .offPeak, at: bj(2026, 1, 5, 12, 0))
        #expect(text.title == "deepseek-flash 转入空闲价")
        #expect(text.body == "输入 ¥1/M · 输出 ¥4/M(省 50%)")
    }

    @Test func peakContentHasNoDiscount() {
        let text = PeakNotifier.notificationContent(price: makePrice(), switchingTo: .peak, at: bj(2026, 1, 5, 14, 0))
        #expect(text.title == "deepseek-flash 转入高峰价")
        #expect(text.body == "输入 ¥2/M · 输出 ¥8/M")
    }
}

// MARK: - Widget 桥接载荷

struct WidgetBridgePayloadTests {
    @Test func roundTripsPeakPrices() throws {
        let payload = WidgetBridgePayload(
            baseURL: "http://127.0.0.1:9192",
            sourceName: "本地",
            summary: nil,
            costToday: nil,
            modelTop: [],
            channelBalances: [],
            peakPrices: [makePrice()],
            billingCurrency: "CNY",
            billingRate: 7.2,
            generatedAt: Date(timeIntervalSince1970: 1_700_000_000)
        )

        let data = try JSONEncoder().encode(payload)
        let back = try JSONDecoder().decode(WidgetBridgePayload.self, from: data)
        #expect(back.peakPrices.count == 1)
        #expect(back.peakPrices[0].model == "deepseek-flash")
        #expect(back.peakPrices[0].peakRule != nil)
        #expect(back.billingCurrency == "CNY")
        #expect(back.billingRate == 7.2)
    }
}

// MARK: - 节假日记号 !H / W(镜像 internal/tests/model/peak_window_holiday_test.go)

/// 2026 国庆假期年历样本:10-01(周四)~10-07 放假,10-10(周六)调休补班。
private let holidaySample = HolidayCalendar(items: [
    HolidayDateItem(date: "2026-10-01", kind: "holiday"),
    HolidayDateItem(date: "2026-10-02", kind: "holiday"),
    HolidayDateItem(date: "2026-10-03", kind: "holiday"),
    HolidayDateItem(date: "2026-10-04", kind: "holiday"),
    HolidayDateItem(date: "2026-10-05", kind: "holiday"),
    HolidayDateItem(date: "2026-10-06", kind: "holiday"),
    HolidayDateItem(date: "2026-10-07", kind: "holiday"),
    HolidayDateItem(date: "2026-10-10", kind: "workday"),
])

struct PeakWindowHolidayTests {
    @Test func parsesHolidayTokens() {
        for rule in [
            "1-5,!H;09:00-12:00,14:00-18:00", // DeepSeek 口径:节假日全天空闲
            "1-5,W;09:00-12:00,14:00-18:00",  // 补班日按工作日
            "1-5,!H,W;09:00-12:00",           // 两个记号组合
            "!H;00:00-23:59",                 // 只有记号、无星期
            "1-5,!h;09:00-12:00",             // 小写容忍
            "1-5,w;09:00-12:00",
        ] {
            #expect(PeakWindow(rule) != nil, "应能解析: \(rule)")
        }
        for rule in [
            "1-5,!X;09:00-12:00", // 未知排除记号
            "1-5,H;09:00-12:00",  // 少了 ! 也应非法(提示用 !H)
        ] {
            #expect(PeakWindow(rule) == nil, "应判非法: \(rule)")
        }
    }

    @Test func skipHolidayTokenTreatsHolidayAsOffPeak() {
        let withToken = try! #require(PeakWindow("1-5,!H;09:00-12:00,14:00-18:00"))
        // 2026-10-01(周四)10:00 落在高峰窗口内,但放假全天空闲
        #expect(withToken.period(at: bj(2026, 10, 1, 10, 0), calendar: holidaySample) == .offPeak)
        #expect(withToken.period(at: bj(2026, 10, 1, 20, 0), calendar: holidaySample) == .offPeak)

        // 未配 !H:维持原口径(按星期判定为高峰)
        let noToken = try! #require(PeakWindow("1-5;09:00-12:00,14:00-18:00"))
        #expect(noToken.period(at: bj(2026, 10, 1, 10, 0), calendar: holidaySample) == .peak)

        // 普通工作日 2026-10-13(周二)不受影响
        #expect(withToken.period(at: bj(2026, 10, 13, 10, 0), calendar: holidaySample) == .peak)
    }

    @Test func makeupWorkdayTokenTreatsShiftedSaturdayAsWorkday() {
        let withToken = try! #require(PeakWindow("1-5,!H,W;09:00-12:00,14:00-18:00"))
        // 2026-10-10(周六)是国庆后补班:白天按工作日计高峰,午休不计
        #expect(withToken.period(at: bj(2026, 10, 10, 10, 0), calendar: holidaySample) == .peak)
        #expect(withToken.period(at: bj(2026, 10, 10, 13, 0), calendar: holidaySample) == .offPeak)

        // 未配 W:周末一律空闲
        let noToken = try! #require(PeakWindow("1-5,!H;09:00-12:00,14:00-18:00"))
        #expect(noToken.period(at: bj(2026, 10, 10, 10, 0), calendar: holidaySample) == .offPeak)

        // 普通周六 2026-10-17 即使配了 W 也照常空闲
        #expect(withToken.period(at: bj(2026, 10, 17, 10, 0), calendar: holidaySample) == .offPeak)
    }

    @Test func tokensFallBackToWeekdayWithoutCalendar() {
        let window = try! #require(PeakWindow("1-5,!H,W;09:00-12:00,14:00-18:00"))
        // 无年历(nil):放假日按普通周四判高峰、补班周六按周末判空闲(与后端退化口径一致)
        #expect(window.period(at: bj(2026, 10, 1, 10, 0), calendar: nil) == .peak)
        #expect(window.period(at: bj(2026, 10, 10, 10, 0), calendar: nil) == .offPeak)
    }

    @Test func nextSwitchSkipsLongHoliday() {
        let window = try! #require(PeakWindow("1-5,!H;09:00-12:00,14:00-18:00"))
        // 2026-09-30(周三)18:30 收盘后:10-01~10-07 放假全天空闲、不产生切换点,
        // 下一次切换是假期结束后的 10-08(周四)09:00 转高峰。
        let next = try! #require(window.nextSwitch(after: bj(2026, 9, 30, 18, 30), calendar: holidaySample))
        #expect(next.at == bj(2026, 10, 8, 9, 0))
        #expect(next.to == .peak)

        // 假期内任意时刻:同样等到 10-08 09:00
        let inside = try! #require(window.nextSwitch(after: bj(2026, 10, 2, 10, 0), calendar: holidaySample))
        #expect(inside.at == bj(2026, 10, 8, 9, 0))
    }

    @Test func holidayCalendarRoundTripInBridgePayload() {
        // 年历随桥载荷序列化:未来 60 天清单导出 → 消费端原样还原
        let items = holidaySample.entries(withinDays: 60, from: bj(2026, 9, 28, 0, 0))
        #expect(!items.isEmpty)
        let restored = HolidayCalendar(items: items)
        #expect(restored.kind(of: "2026-10-01") == .holiday)
        #expect(restored.kind(of: "2026-10-10") == .workday)
    }

    @Test func eveSwitchLandsAtHolidayOnset() throws {
        // 假期前一天的下一次切换应落在假期开始(转谷):假期前夜的收尾高峰窗口在 23:59
        // 收尾(Go 的 [起,止) 语义下 23:59 起已是空闲),全天空闲的假期由此开始。
        let window = try #require(PeakWindow("1-5,!H;09:00-23:59"))
        let next = try #require(window.nextSwitch(after: bj(2026, 9, 30, 22, 0), calendar: holidaySample))
        #expect(next.at == bj(2026, 9, 30, 23, 59))
        #expect(next.to == .offPeak)

        // 其后的切换跳过整个假期:落在节后首个工作日 10-08 的窗口起点(转高峰)
        let afterEve = try #require(window.nextSwitch(after: next.at, calendar: holidaySample))
        #expect(afterEve.at == bj(2026, 10, 8, 9, 0))
        #expect(afterEve.to == .peak)

        // DeepSeek 口径:假期前天下班(最后一个高峰窗口收尾)同样是一次转谷切换
        let deepseek = try #require(PeakWindow("1-5,!H;09:00-12:00,14:00-18:00"))
        let clockOff = try #require(deepseek.nextSwitch(after: bj(2026, 9, 30, 15, 0), calendar: holidaySample))
        #expect(clockOff.at == bj(2026, 9, 30, 18, 0))
        #expect(clockOff.to == .offPeak)
    }

    @Test func makeupWorkdaySwitchesMatchWorkday() throws {
        // 补班日(10-10 周六)的切换与工作日一致:早晨 09:00 转高峰、午休 12:00 转空闲。
        let window = try #require(PeakWindow("1-5,!H,W;09:00-12:00,14:00-18:00"))
        let morning = try #require(window.nextSwitch(after: bj(2026, 10, 9, 20, 0), calendar: holidaySample))
        #expect(morning.at == bj(2026, 10, 10, 9, 0))
        #expect(morning.to == .peak)

        let lunch = try #require(window.nextSwitch(after: bj(2026, 10, 10, 10, 0), calendar: holidaySample))
        #expect(lunch.at == bj(2026, 10, 10, 12, 0))
        #expect(lunch.to == .offPeak)

        // 未配 W:补班周六不产生切换点,下一次切换跳到节后周一 09:00
        let noToken = try #require(PeakWindow("1-5,!H;09:00-12:00,14:00-18:00"))
        let skipped = try #require(noToken.nextSwitch(after: bj(2026, 10, 9, 20, 0), calendar: holidaySample))
        #expect(skipped.at == bj(2026, 10, 12, 9, 0))
        #expect(skipped.to == .peak)
    }
}
