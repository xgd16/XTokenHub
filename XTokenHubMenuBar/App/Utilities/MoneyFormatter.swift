import Foundation

/// 金额展示工具,口径与前端 `frontend/src/utils/money.ts` 对齐。
///
/// 全链路以 USD 存储(后端 cost_usd),这里只负责换汇与格式化;汇率由设置页手工配置。
enum MoneyFormatter {
    /// 展示币种与汇率。CNY 但汇率为 0 时回退 USD,避免把美元数字套上 ¥。
    struct Options: Equatable, Sendable {
        var currency: String
        var rate: Double

        static let usd = Options(currency: "USD", rate: 0)

        var normalized: Options {
            if currency == "CNY" && !(rate > 0) { return .usd }
            return self
        }
    }

    /// 花费币种符号;未知/空值按 USD 处理。
    static func symbol(for currency: String?) -> String {
        currency == "CNY" ? "¥" : "$"
    }

    /// 上游余额币种符号:未识别币种原样带尾部空格展示(如 "EUR ")。
    /// 余额是上游账户真实币种,不做汇率折算。
    static func balanceSymbol(for currency: String?) -> String {
        guard let currency, !currency.isEmpty else { return "$" }
        switch currency {
        case "CNY": return "¥"
        case "USD": return "$"
        default: return "\(currency) "
        }
    }

    /// USD 金额换算为目标币种数值。
    static func convert(usd: Double, options: Options) -> Double {
        guard usd.isFinite else { return 0 }
        let o = options.normalized
        return o.currency == "CNY" ? usd * o.rate : usd
    }

    /// 金额小数位:金额越大越省位,小额保留 4 位以便看清小额花费。
    static func digits(for value: Double) -> Int {
        let magnitude = abs(value)
        if magnitude >= 100 { return 2 }
        if magnitude >= 1 { return 3 }
        return 4
    }

    /// 格式化金额(USD 入参,按 options 换算后展示)。
    /// 零值 → 符号 + 0;无效值 → 符号 + —,便于区分「没有花费」与「数据缺失」。
    static func format(usd: Double, options: Options = .usd) -> String {
        let o = options.normalized
        let sym = symbol(for: o.currency)
        guard usd.isFinite else { return sym + "—" }
        let value = convert(usd: usd, options: o)
        if value == 0 { return sym + "0" }

        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        formatter.locale = Locale(identifier: "en_US")
        formatter.usesGroupingSeparator = true
        let n = digits(for: value)
        formatter.minimumFractionDigits = n
        formatter.maximumFractionDigits = n
        return sym + (formatter.string(from: NSNumber(value: value)) ?? String(value))
    }

    /// 上游余额展示:原币种符号 + 固定两位小数。
    static func formatBalance(_ total: Double, currency: String?) -> String {
        let sym = balanceSymbol(for: currency)
        guard total.isFinite else { return sym + "—" }
        return sym + String(format: "%.2f", total)
    }
}
