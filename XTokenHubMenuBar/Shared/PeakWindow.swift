import Foundation

// MARK: - 计价时段

/// 计价时段（与后端 internal/model/peak_window.go 的 PricePeriod 对齐）。
enum PricePeriod: String, Sendable {
    case peak
    case offPeak = "off_peak"

    /// 中文短标签（弹出层/小组件共用）。
    var label: String {
        switch self {
        case .peak: "高峰价"
        case .offPeak: "空闲价"
        }
    }
}

// MARK: - 高峰时段规则

/// 高峰时段规则，Go `internal/model/peak_window.go` 的 Swift 镜像，供主 App 与 Widget 共用。
///
/// 形态 `<星期>;<时段>`：
/// - 星期 `1=周一 .. 7=周日`，支持 `1-5` 区间与 `1,6,7` 列表；
/// - 时段 `09:00-12:00`，多个用逗号分隔；按 [start, end) 判定，结束须晚于开始（不跨零点）；
/// - 规则「列出高峰窗口」，未列出即空闲时段。
///
/// 判定统一按北京时间：固定 UTC+8（中国无夏令时，固定偏移永远正确，
/// 且不依赖设备 tzdata）。空串或非法规则返回 nil，按「未配置时段价」处理。
struct PeakWindow: Equatable, Sendable {
    /// 北京时间固定 UTC+8，与后端 FixedZone 同口径。
    static let beijingTimeZone = TimeZone(secondsFromGMT: 8 * 3600)!

    private static var beijingCalendar: Calendar {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = beijingTimeZone
        cal.locale = Locale(identifier: "en_US_POSIX")
        return cal
    }

    /// 原始规则串。
    let raw: String
    /// 下标 1..7 有效（1=周一）。
    private let days: [Bool]
    /// 每段 [起始分钟, 结束分钟)。
    private let spans: [Range<Int>]

    init?(_ rule: String) {
        let s = rule.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !s.isEmpty else { return nil }
        let parts = s.split(separator: ";", omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 2,
              let days = Self.parseWeekdays(parts[0].trimmingCharacters(in: .whitespaces)),
              let spans = Self.parseSpans(parts[1].trimmingCharacters(in: .whitespaces)),
              !spans.isEmpty else { return nil }
        self.raw = s
        self.days = days
        self.spans = spans
    }

    /// 判定 at 时刻（任意时区的同一瞬间）是否处于高峰时段。
    func isPeak(at date: Date) -> Bool {
        let cal = Self.beijingCalendar
        let c = cal.dateComponents([.weekday, .hour, .minute], from: date)
        // Calendar 的 weekday 1=周日；规则用 1=周一 .. 7=周日。
        let day = (c.weekday ?? 0) == 1 ? 7 : (c.weekday ?? 0) - 1
        guard day >= 1, day <= 7, days[day] else { return false }
        let minute = (c.hour ?? 0) * 60 + (c.minute ?? 0)
        return spans.contains { minute >= $0.lowerBound && minute < $0.upperBound }
    }

    /// at 时刻的计价时段。规则已解析成功，恒有值。
    func period(at date: Date) -> PricePeriod {
        isPeak(at: date) ? .peak : .offPeak
    }

    /// 下一次切换：切换时刻与切入的时段。规则恒为高峰（如 `1-7;00:00-23:59`）时返回 nil。
    ///
    /// 只需枚举未来 8 天内每个高峰窗口的起止点（起点切到高峰、终点切到空闲），
    /// 取其中最早且晚于 after 的一个——窗口按「列出高峰」表达，非高峰即空闲，
    /// 这两类边界构成全部切换点；规则以星期为周期，8 天必覆盖。
    func nextSwitch(after date: Date) -> (at: Date, to: PricePeriod)? {
        let cal = Self.beijingCalendar
        let dayComps = cal.dateComponents([.year, .month, .day], from: date)
        guard let day0 = cal.date(from: dayComps) else { return nil }

        var best: (at: Date, to: PricePeriod)?
        for offset in 0...8 {
            guard let day = cal.date(byAdding: .day, value: offset, to: day0) else { continue }
            let dc = cal.dateComponents([.year, .month, .day, .weekday], from: day)
            let weekday = (dc.weekday ?? 0) == 1 ? 7 : (dc.weekday ?? 0) - 1
            guard weekday >= 1, weekday <= 7, days[weekday] else { continue }
            for span in spans {
                let candidates: [(minute: Int, to: PricePeriod)] = [
                    (span.lowerBound, .peak),
                    (span.upperBound, .offPeak),
                ]
                for candidate in candidates {
                    var comps = dc
                    comps.hour = candidate.minute / 60
                    comps.minute = candidate.minute % 60
                    comps.second = 0
                    guard let at = cal.date(from: comps), at > date else { continue }
                    if best == nil || at < best!.at {
                        best = (at, candidate.to)
                    }
                }
            }
        }
        return best
    }

    // MARK: - 解析（语义与 Go parseWeekdays/parseSpans/parseClock 一一对应）

    private static func parseWeekdays(_ s: String) -> [Bool]? {
        guard !s.isEmpty else { return nil }
        var days = [Bool](repeating: false, count: 8)
        for item in s.split(separator: ",", omittingEmptySubsequences: false) {
            let trimmed = item.trimmingCharacters(in: .whitespaces)
            guard !trimmed.isEmpty else { return nil }
            var loStr = trimmed
            var hiStr = trimmed
            if let dash = trimmed.firstIndex(of: "-") {
                loStr = String(trimmed[..<dash]).trimmingCharacters(in: .whitespaces)
                hiStr = String(trimmed[dash...].dropFirst()).trimmingCharacters(in: .whitespaces)
            }
            guard let lo = weekday(loStr), let hi = weekday(hiStr), lo <= hi else { return nil }
            for d in lo...hi {
                days[d] = true
            }
        }
        return days
    }

    private static func weekday(_ s: String) -> Int? {
        guard let n = Int(s), (1...7).contains(n) else { return nil }
        return n
    }

    private static func parseSpans(_ s: String) -> [Range<Int>]? {
        guard !s.isEmpty else { return nil }
        var spans: [Range<Int>] = []
        for item in s.split(separator: ",", omittingEmptySubsequences: false) {
            let trimmed = item.trimmingCharacters(in: .whitespaces)
            guard !trimmed.isEmpty, let dash = trimmed.firstIndex(of: "-") else { return nil }
            guard let start = clock(String(trimmed[..<dash])),
                  let end = clock(String(trimmed[dash...].dropFirst())),
                  end > start else { return nil }
            spans.append(start..<end)
        }
        return spans
    }

    /// HH:MM（或 H:MM）→ 当日分钟数，值域 00:00..23:59。
    private static func clock(_ s: String) -> Int? {
        let parts = s.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count == 2,
              let h = Int(parts[0].trimmingCharacters(in: .whitespaces)), (0...23).contains(h),
              let m = Int(parts[1].trimmingCharacters(in: .whitespaces)), (0...59).contains(m) else { return nil }
        return h * 60 + m
    }
}

/// 峰谷切换倒计时文案：2d3h / 1h35m / 47m（与 Web 端卡片同风格）。主 App 与 Widget 共用。
func peakCountdown(from: Date, to: Date) -> String {
    let totalMin = max(0, Int((to.timeIntervalSince(from) / 60).rounded()))
    let days = totalMin / 1440
    let hours = totalMin % 1440 / 60
    let minutes = totalMin % 60
    if days > 0 { return "\(days)d\(hours)h" }
    if hours > 0 { return "\(hours)h\(minutes)m" }
    return "\(minutes)m"
}
