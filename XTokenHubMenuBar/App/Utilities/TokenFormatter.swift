import Foundation

/// 数字格式化 —— 单位方式与 Web 前端 `frontend/src/utils/format.ts` 完全一致。
enum TokenFormatter {
    /// 大数中文单位(Web compactCN):>=1亿 → X亿;>=1万 → X万;否则四舍五入原数。
    /// 最多保留 2 位小数并去掉尾随零(1.20 → 1.2)。
    static func compact(_ value: Int64) -> String {
        let sign = value < 0 ? "-" : ""
        let abs = Swift.abs(value)
        if abs >= 100_000_000 { return sign + trimmed(Double(abs) / 100_000_000) + "亿" }
        if abs >= 10_000 { return sign + trimmed(Double(abs) / 10_000) + "万" }
        return sign + String(abs)
    }

    /// 实时吞吐数值部分,对应 Web 端 `compactCN(Math.round(tps))` + " tok/s"。
    static func compactSpeed(_ tokensPerSec: Double) -> String {
        compact(Int64(tokensPerSec.rounded()))
    }

    /// 耗时(Web duration()):<1000ms → 取整 ms;否则保留 2 位小数的秒。
    static func duration(_ ms: Int64) -> String {
        ms < 1_000 ? "\(ms)ms" : String(format: "%.2fs", Double(ms) / 1_000)
    }

    /// 百分数(Web percent()):0.234 → "23.4%"。
    static func percent(_ ratio: Double, digits: Int = 1) -> String {
        guard ratio.isFinite else { return "0%" }
        return String(format: "%.\(digits)f%%", ratio * 100)
    }

    /// 输出速度(Web tokenSpeed()):completion 数 / 耗时;任一侧无效(进行中)返回 —。
    static func speed(completionTokens: Int64, durationMS: Int64) -> String {
        guard completionTokens > 0, durationMS > 0 else { return "—" }
        let perSec = Double(completionTokens) / Double(durationMS) * 1_000
        return compact(Int64(perSec.rounded())) + " tok/s"
    }

    /// 拆出 `compact(_:)` 的中文数量级后缀(万/亿),便于统计磁贴把单位排成次级字号。
    /// 无数量级(如 980)时 unit 为空串。
    static func splitCompact(_ value: Int64) -> (number: String, unit: String) {
        let text = compact(value)
        guard let suffix = text.last, suffix == "万" || suffix == "亿" else {
            return (text, "")
        }
        return (String(text.dropLast()), String(suffix))
    }

    /// 对应 JS `parseFloat(x.toFixed(2))`:保留最多 2 位小数并去掉尾随零。
    private static func trimmed(_ x: Double) -> String {
        var s = String(format: "%.2f", x)
        if s.contains(".") {
            while s.hasSuffix("0") { s.removeLast() }
            if s.hasSuffix(".") { s.removeLast() }
        }
        return s
    }
}
