import Foundation

/// 请求日志的展示映射 —— 口径与 Web 端 `frontend/src/utils/format.ts` 对齐,
/// 供实时请求流把协议/模式/客户端 UA 折叠成短名。
enum RequestLogDisplay {
    /// 协议短名(Web protocolShort)。
    static func protocolShort(_ protocolName: String) -> String {
        switch protocolName {
        case "chat_completions": "chat"
        case "responses": "responses"
        case "messages": "messages"
        default: protocolName
        }
    }

    /// 转发模式短名(Web modeShort)。
    static func modeShort(_ forwardMode: String) -> String {
        switch forwardMode {
        case "native_passthrough": "透传"
        case "converted": "转换"
        default: forwardMode
        }
    }

    /// 浏览器 UA(Mozilla 前缀)的产品识别规则。
    private static let browserPatterns: [(pattern: String, label: String)] = [
        ("Edg/", "Edge"),
        ("OPR/", "Opera"),
        ("Chrome/", "Chrome"),
        ("Firefox/", "Firefox"),
        ("Safari/", "Safari"),
    ]

    /// 认得出的 agent 工具/SDK 映射成友好名。
    private static let toolPatterns: [(pattern: String, label: String, anchored: Bool)] = [
        ("claude-cli|claude-code", "Claude Code", false),
        ("codex", "Codex CLI", false),
        ("^hermes", "Hermes Agent", true),
        ("^deepseek", "DeepSeek Harness", true),
        ("gemini-cli", "Gemini CLI", false),
        ("\\bgoose\\b", "Goose", false),
        ("\\bcline\\b", "Cline", false),
        ("roo-?code", "Roo Code", false),
        ("cursor", "Cursor", false),
        ("cherrystudio", "Cherry Studio", false),
        ("lobehub|lobe-chat", "LobeChat", false),
        ("nextchat", "NextChat", false),
        ("chatbox", "ChatBox", false),
        ("dify", "Dify", false),
        ("openai", "OpenAI SDK", false),
        ("anthropic", "Anthropic SDK", false),
    ]

    /// 从 User-Agent 提取调用方工具短名(Web agentShort):
    /// 取首个「产品/版本」段,能识别的工具换友好名;认不出的原样保留产品名。
    /// 刻意不把 python-httpx、go-http-client 之类折叠成实现语言。
    static func agentShort(_ userAgent: String) -> String {
        let trimmed = userAgent.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return "—" }

        // split(/[\s(]/) 取首段:空格或左括号截断
        let first = trimmed.prefix { !$0.isWhitespace && $0 != "(" }
        guard !first.isEmpty else { return "—" }
        let head = String(first)

        let slash = head.firstIndex(of: "/")
        let name = slash.map { String(head[head.startIndex..<$0]) } ?? head

        if name.lowercased() == "mozilla" {
            for rule in browserPatterns where trimmed.contains(rule.pattern) {
                return rule.label
            }
        }

        // 版本号形如 3.11.2 才带上,避免 "OpenAI/NodeJS/5.1" 这类多段产品串一起显示
        var suffix = ""
        if let slash {
            let version = String(head[head.index(after: slash)...])
            let parts = version.split(separator: ".", omittingEmptySubsequences: false)
            if !version.isEmpty, parts.allSatisfy({ !$0.isEmpty && $0.allSatisfy(\.isNumber) }) {
                suffix = "/" + version
            }
        }

        for rule in toolPatterns where matches(name, rule.pattern, anchored: rule.anchored) {
            return rule.label + suffix
        }

        return head.isEmpty ? "—" : String(head.prefix(32))
    }

    /// 时间 HH:mm:ss(Web timeOf,本地时区)。
    static func clock(_ date: Date, calendar: Calendar = .current) -> String {
        let c = calendar.dateComponents([.hour, .minute, .second], from: date)
        return String(format: "%02d:%02d:%02d", c.hour ?? 0, c.minute ?? 0, c.second ?? 0)
    }

    /// 完整时间 MM-dd HH:mm:ss(Web fullTime,本地时区)。
    static func fullClock(_ date: Date, calendar: Calendar = .current) -> String {
        let c = calendar.dateComponents([.month, .day, .hour, .minute, .second], from: date)
        return String(format: "%02d-%02d %02d:%02d:%02d", c.month ?? 0, c.day ?? 0, c.hour ?? 0, c.minute ?? 0, c.second ?? 0)
    }

    private static func matches(_ text: String, _ pattern: String, anchored: Bool) -> Bool {
        var options: String.CompareOptions = [.regularExpression, .caseInsensitive]
        if anchored { options.insert(.anchored) }
        return text.range(of: pattern, options: options) != nil
    }
}

// MARK: - RequestLog 展示属性

extension RequestLog {
    var protocolLabel: String { RequestLogDisplay.protocolShort(protocolName) }
    var modeLabel: String { RequestLogDisplay.modeShort(forwardMode) }
    var clientLabel: String { RequestLogDisplay.agentShort(userAgent) }

    /// 是否发生缓存命中(0 表示无缓存计量,Web 端同样显示 —)。
    var hasCacheHit: Bool { cachedTokens > 0 }

    /// 入/出 token 简写,如 "↑1.2万 ↓3.4千"。
    var tokenInOutText: String {
        "↑\(TokenFormatter.compact(promptTokens)) ↓\(TokenFormatter.compact(completionTokens))"
    }

    /// 单次花费(按展示币种换算;缺 cost_usd 的旧后端按 0 处理)。
    func costText(money: MoneyFormatter.Options) -> String {
        MoneyFormatter.format(usd: costUSD ?? 0, options: money)
    }

    /// 用户可见时间;解析失败时回退原始字符串(与 Web 端一致)。
    var clockText: String {
        guard let date = loggedAt else { return createdAt }
        return RequestLogDisplay.clock(date)
    }

    var fullClockText: String {
        guard let date = loggedAt else { return createdAt }
        return RequestLogDisplay.fullClock(date)
    }
}
