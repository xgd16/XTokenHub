import Foundation

/// Go 后端时间字符串解析:RFC3339(带/不带小数秒)与 "YYYY-MM-DD" 日期键。
enum HubDate {
    /// 解析频率低(日志行级),直接每次新建 formatter 避免共享可变状态。
    static func parseRFC3339(_ s: String) -> Date? {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = fractional.date(from: s) { return d }
        return ISO8601DateFormatter().date(from: s)
    }

    /// "2026-09-08" → 本地时区当日零点;格式异常时返回当前时间。
    static func parseDay(_ s: String) -> Date {
        let comps = s.split(separator: "-").compactMap { Int($0) }
        guard comps.count == 3,
              let date = Calendar.current.date(from: DateComponents(year: comps[0], month: comps[1], day: comps[2]))
        else { return Date() }
        return date
    }
}
