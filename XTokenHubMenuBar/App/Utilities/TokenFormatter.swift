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
