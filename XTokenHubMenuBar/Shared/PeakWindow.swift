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
///   另有两个节假日记号（判定依赖节假日年历 `HolidayCalendar`，与后端 holiday.go 同口径）：
///   `!H` 放假日（含调休连休）全天不计高峰；`W` 调休补班日按工作日对待；
/// - 时段 `09:00-12:00`，多个用逗号分隔；按 [start, end) 判定，结束须晚于开始（不跨零点）；
/// - 规则「列出高峰窗口」，未列出即空闲时段。
///
/// 例（DeepSeek 工作日高峰、节假日全天空闲）：`1-5,!H;09:00-12:00,14:00-18:00`。
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
    /// `!H`：放假日（kind=holiday，含调休连休）全天不计高峰。
    private let skipHoliday: Bool
    /// `W`：调休补班日（kind=workday，周末但上班）视为命中星期集合。
    private let makeupWorkday: Bool

    init?(_ rule: String) {
        let s = rule.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !s.isEmpty else { return nil }
        let parts = s.split(separator: ";", omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 2,
              let dayPattern = Self.parseWeekdays(parts[0].trimmingCharacters(in: .whitespaces)),
              let spans = Self.parseSpans(parts[1].trimmingCharacters(in: .whitespaces)),
              !spans.isEmpty else { return nil }
        self.raw = s
        self.days = dayPattern.days
        self.spans = spans
        self.skipHoliday = dayPattern.skipHoliday
        self.makeupWorkday = dayPattern.makeupWorkday
    }

    /// 判定 at 时刻（任意时区的同一瞬间）是否处于高峰时段。
    ///
    /// 判定顺序与后端 `IsPeak` 逐条对应：先按 `!H` 记号修正（放假日全天空闲，
    /// 优先于星期与时段），再判星期集合与 `W` 记号（补班日视为命中星期），
    /// 最后按 [起, 止) 分钟窗判定。`calendar` 缺省取进程内注入的年历
    /// （`HolidaySourceStore`）；无年历数据时记号退化为按星期判定。
    func isPeak(at date: Date, calendar: HolidayCalendar? = HolidaySourceStore.shared.calendar) -> Bool {
        let cal = Self.beijingCalendar
        let c = cal.dateComponents([.year, .month, .day, .weekday, .hour, .minute], from: date)
        let dayString = Self.beijingDayString(c)
        // `!H`：放假日（含调休连休）全天空闲，优先于星期与时段判定。
        if skipHoliday, calendar?.kind(of: dayString) == .holiday {
            return false
        }
        // Calendar 的 weekday 1=周日；规则用 1=周一 .. 7=周日。
        let rawWeekday = c.weekday ?? 0
        let day = rawWeekday == 1 ? 7 : rawWeekday - 1
        var matched = day >= 1 && day <= 7 && days[day]
        // `W`：调休补班日（周末但按工作日上班）视为命中星期集合。
        if !matched, makeupWorkday, calendar?.kind(of: dayString) == .workday {
            matched = true
        }
        guard matched else { return false }
        let minute = (c.hour ?? 0) * 60 + (c.minute ?? 0)
        return spans.contains { minute >= $0.lowerBound && minute < $0.upperBound }
    }

    /// at 时刻的计价时段。规则已解析成功，恒有值。
    func period(at date: Date, calendar: HolidayCalendar? = HolidaySourceStore.shared.calendar) -> PricePeriod {
        isPeak(at: date, calendar: calendar) ? .peak : .offPeak
    }

    /// 下一次切换：切换时刻与切入的时段。规则没有任何切换点时返回 nil。
    ///
    /// 切换点是 `period` 真正变化的时刻，只可能出现在高峰窗口起止处（含 00:00 起点）：
    /// 枚举未来 31 天内的这些边界与跨日边界，取其中「边界前后时段不同」且严格晚于
    /// after 的最早一个。这样节假日记号自动生效——放假日（`!H`）整天空闲，
    /// 其内部边界前后都是空闲，天然不产生切换点；补班日（`W`）则像工作日一样
    /// 产生窗口起止切换点。31 天覆盖最长连休（春节 8~9 天）加相邻周末。
    func nextSwitch(after date: Date, calendar: HolidayCalendar? = HolidaySourceStore.shared.calendar) -> (at: Date, to: PricePeriod)? {
        let cal = Self.beijingCalendar
        let dayComps = cal.dateComponents([.year, .month, .day], from: date)
        guard let day0 = cal.date(from: dayComps) else { return nil }

        var best: (at: Date, to: PricePeriod)?
        for offset in 0...31 {
            guard let day = cal.date(byAdding: .day, value: offset, to: day0) else { continue }
            let dc = cal.dateComponents([.year, .month, .day, .weekday], from: day)
            // 候选边界：跨日 00:00 + 本日各高峰窗口的起止（与 00:00 重复也无妨）。
            var minutes: [Int] = [0]
            for span in spans {
                minutes.append(span.lowerBound)
                minutes.append(span.upperBound)
            }
            for minute in minutes {
                var comps = dc
                comps.hour = minute / 60
                comps.minute = minute % 60
                comps.second = 0
                guard let at = cal.date(from: comps), at > date else { continue }
                let to = period(at: at, calendar: calendar)
                // 只保留真实切换：边界前后同一时段（放假日内部、相接的两段窗口）不算切换点。
                let from = period(at: at.addingTimeInterval(-1), calendar: calendar)
                if from == to { continue }
                if best == nil || at < best!.at {
                    best = (at, to)
                }
            }
        }
        return best
    }

    // MARK: - 北京日期（年历主键口径，与后端 beijingDate 同格式）

    /// at 时刻的北京日期 YYYY-MM-DD。
    static func beijingDayString(of date: Date) -> String {
        beijingDayString(beijingCalendar.dateComponents([.year, .month, .day], from: date))
    }

    private static func beijingDayString(_ c: DateComponents) -> String {
        String(format: "%04d-%02d-%02d", c.year ?? 0, c.month ?? 0, c.day ?? 0)
    }

    // MARK: - 解析（语义与 Go parseWeekdays/parseSpans/parseClock 一一对应）

    private static func parseWeekdays(_ s: String) -> (days: [Bool], skipHoliday: Bool, makeupWorkday: Bool)? {
        guard !s.isEmpty else { return nil }
        var days = [Bool](repeating: false, count: 8)
        var skipHoliday = false
        var makeupWorkday = false
        for item in s.split(separator: ",", omittingEmptySubsequences: false) {
            let trimmed = item.trimmingCharacters(in: .whitespaces)
            guard !trimmed.isEmpty else { return nil }
            // 节假日记号（大小写不敏感）：`!H` 放假日不计峰、`W` 补班日按工作日。
            switch trimmed.uppercased() {
            case "!H":
                skipHoliday = true
                continue
            case "W":
                makeupWorkday = true
                continue
            default:
                break
            }
            // 其余 `!` 开头的都是未知节假日记号（如 !X，少了 ! 的 H 同样报错）。
            if trimmed.hasPrefix("!") { return nil }
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
        return (days, skipHoliday, makeupWorkday)
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

// MARK: - 节假日年历（`!H` / `W` 记号的判定依据）

/// 节假日日期类型（与后端 `internal/model/holiday.go` 的 HolidayKind 同口径）。
enum HolidayKind: String, Codable, Sendable {
    /// 放假（含调休连休的全部放假日，非仅法定节假日当天）。
    case holiday
    /// 调休补班上班日（周末但按工作日上班）。
    case workday
}

/// 节假日标注条目（GET /api/v1/settings/holidays 的 items 元素；
/// 也兼作 Widget 桥接载荷里的「未来 60 天日期清单」）。
struct HolidayDateItem: Codable, Sendable, Equatable {
    /// 北京日期 YYYY-MM-DD。
    var date: String
    /// holiday=放假（含调休连休）| workday=调休补班上班。
    var kind: String
}

/// 节假日年历：北京日期 YYYY-MM-DD → 标注。无记录的日期按普通日期处理。
///
/// 年历是纯值，Widget 与 App 各自注入（见 `HolidaySourceStore`）；
/// 空年历与「未拿到年历」行为一致——记号退化为按星期判定，与后端
/// 「年历还没有数据时记号退化」同口径。
struct HolidayCalendar: Sendable, Equatable {
    /// 判定所需的前瞻天数：Widget 离线判对所需、也是年份拉取的跨界依据。
    static let horizonDays = 60

    /// 日期 → 标注。
    let kinds: [String: HolidayKind]

    init(kinds: [String: HolidayKind] = [:]) {
        self.kinds = kinds
    }

    /// 从接口/桥接的条目清单构造（未知 kind 忽略，不因脏数据抛错）。
    init(items: [HolidayDateItem]) {
        var kinds: [String: HolidayKind] = [:]
        for item in items {
            if let kind = HolidayKind(rawValue: item.kind) {
                kinds[item.date] = kind
            }
        }
        self.kinds = kinds
    }

    /// 查标注；普通日期返回 nil。
    func kind(of beijingDay: String) -> HolidayKind? {
        kinds[beijingDay]
    }

    /// 合并两张年历（同日期以新表为准）。
    func merging(_ other: HolidayCalendar) -> HolidayCalendar {
        var merged = kinds
        for (day, kind) in other.kinds {
            merged[day] = kind
        }
        return HolidayCalendar(kinds: merged)
    }

    /// 起自 from 的未来 days 天（含当天）内的标注清单，按日期升序——
    /// Widget 离线判定所需的最小年历，随桥接载荷携带。
    func entries(withinDays days: Int = HolidayCalendar.horizonDays, from date: Date = Date()) -> [HolidayDateItem] {
        let cal = Self.calendar
        let day0 = cal.date(from: cal.dateComponents([.year, .month, .day], from: date))
        var result: [HolidayDateItem] = []
        for offset in 0..<days {
            guard let day = day0.flatMap({ cal.date(byAdding: .day, value: offset, to: $0) }) else { continue }
            let key = PeakWindow.beijingDayString(of: day)
            if let kind = kinds[key] {
                result.append(HolidayDateItem(date: key, kind: kind.rawValue))
            }
        }
        return result
    }

    /// 判定 horizon 天会跨到哪些年份（含今年；年底附近含明年）——
    /// 接口逐年拉取，按这个清单拉就够。
    static func years(coveringHorizonDays days: Int = horizonDays, from date: Date = Date()) -> [Int] {
        let cal = calendar
        let year = Int(PeakWindow.beijingDayString(of: date).prefix(4)) ?? 0
        guard let end = cal.date(byAdding: .day, value: days, to: date) else { return [year] }
        let endYear = Int(PeakWindow.beijingDayString(of: end).prefix(4)) ?? year
        return endYear > year ? [year, endYear] : [year]
    }

    private static var calendar: Calendar {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = PeakWindow.beijingTimeZone
        cal.locale = Locale(identifier: "en_US_POSIX")
        return cal
    }
}

/// 节假日年历注入点（进程级），与后端 `model.SetHolidaySource` / `HolidayKindOf` 同口径。
///
/// 主 App 由 HubStore 拉取接口后注入；Widget 由桥接载荷/直连接口注入。
/// 未注入时 `PeakWindow` 的记号判定退化为按星期判定。
final class HolidaySourceStore: @unchecked Sendable {
    static let shared = HolidaySourceStore()

    private let lock = NSLock()
    private var _calendar: HolidayCalendar?

    /// 当前年历；未注入时 nil。
    var calendar: HolidayCalendar? {
        lock.lock()
        defer { lock.unlock() }
        return _calendar
    }

    /// 注入（nil = 清空，等价后端 SetHolidaySource(nil)）。
    func install(_ calendar: HolidayCalendar?) {
        lock.lock()
        defer { lock.unlock() }
        _calendar = calendar
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
