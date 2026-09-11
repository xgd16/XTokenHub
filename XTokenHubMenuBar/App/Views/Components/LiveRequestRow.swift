import SwiftUI

/// 实时请求流单行:三行富行 —— 协议/模式/模型/状态码/时间、渠道·调用方·客户端、
/// 入出 tokens·缓存命中·输出速度·耗时·花费。对齐 Web 仪表盘实时流的字段。
/// 长尾信息(会话/请求头/IP/计价口径)收进悬停提示,避免 400pt 面板溢出。
struct LiveRequestRow: View {
    let log: RequestLog
    /// 金额展示选项(币种/汇率),与面板其余卡片同源。
    let money: MoneyFormatter.Options

    var body: some View {
        HStack(alignment: .top, spacing: 9) {
            statusIcon
                .frame(width: 18)
            VStack(alignment: .leading, spacing: 3) {
                titleLine
                subtitleLine
                metricsLine
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: 8).fill(rowBackground))
        .help(hoverHelp)
    }

    // MARK: - 第一行:协议/模式徽标 + 模型 + 状态码 + 时间

    private var titleLine: some View {
        HStack(spacing: 5) {
            if !log.protocolName.isEmpty {
                chip(log.protocolLabel, tint: .cyan)
            }
            if !log.forwardMode.isEmpty {
                chip(log.modeLabel, tint: log.forwardMode == "native_passthrough" ? .accentColor : .orange)
            }
            Text(log.model.isEmpty ? "未知模型" : log.model)
                .font(.callout.weight(.medium))
                .lineLimit(1)
                .truncationMode(.middle)
                .layoutPriority(1)
            Spacer(minLength: 6)
            statusCode
            Text(log.clockText)
                .font(.caption2.monospacedDigit())
                .foregroundStyle(.secondary)
                .fixedSize()
        }
    }

    // MARK: - 第二行:渠道 · 调用方 · 客户端(出错时显示错误)

    private var subtitleLine: some View {
        Text(subtitle)
            .font(.caption2)
            .foregroundStyle(log.error.isEmpty ? Color.secondary : Color.red)
            .lineLimit(1)
            .truncationMode(.middle)
    }

    private var subtitle: String {
        if !log.error.isEmpty { return log.error }
        var parts: [String] = []
        if !log.channelName.isEmpty { parts.append(log.channelName) }
        if !log.keyName.isEmpty { parts.append(log.keyName) }
        if !log.userAgent.isEmpty { parts.append(log.clientLabel) }
        return parts.isEmpty ? "—" : parts.joined(separator: " · ")
    }

    // MARK: - 第三行:入出 tokens · 缓存命中 · 输出速度 · 耗时 · 花费

    private var metricsLine: some View {
        metricsText
            .font(.caption2)
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .minimumScaleFactor(0.85)
    }

    /// 拼成单个 Text:命中率需单独着色,同时整体可缩放以免溢出 330pt 行宽。
    private var metricsText: Text {
        var out = AttributedString()
        for (index, segment) in metricsSegments.enumerated() {
            if index > 0 { out += AttributedString(" · ") }
            var piece = AttributedString(segment.text)
            if let tint = segment.tint { piece.foregroundColor = tint }
            out += piece
        }
        return Text(out)
    }

    private var metricsSegments: [(text: String, tint: Color?)] {
        if log.isPending {
            let elapsed = pendingTime
            return [
                ("↑— ↓—", nil),
                ("缓存—", nil),
                ("—", nil),
                (elapsed.isEmpty ? "—" : elapsed, nil),
                ("—", nil),
            ]
        }
        return [
            (log.tokenInOutText, nil),
            ("缓存" + cacheText, cacheTint),
            (TokenFormatter.speed(completionTokens: log.completionTokens, durationMS: log.durationMS), nil),
            (TokenFormatter.duration(log.durationMS), nil),
            (log.costText(money: money), nil),
        ]
    }

    /// 无缓存计量时按 Web 口径显示 —。
    private var cacheText: String {
        log.hasCacheHit ? TokenFormatter.percent(log.cacheHitRate, digits: 0) : "—"
    }

    private var cacheTint: Color? {
        guard log.hasCacheHit else { return nil }
        let rate = log.cacheHitRate
        if rate >= 0.5 { return .accentColor }
        if rate >= 0.2 { return .cyan }
        if rate > 0 { return .orange }
        return nil
    }

    // MARK: - 状态

    private var statusIcon: some View {
        Group {
            if log.isPending {
                ProgressView()
                    .controlSize(.mini)
            } else if log.isError {
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.red)
            } else {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
            }
        }
        .font(.callout)
    }

    private var statusCode: some View {
        Group {
            if log.isPending {
                Text("运行中").foregroundStyle(.orange)
            } else if log.isError {
                Text(statusCodeText).foregroundStyle(.red)
            } else {
                Text(statusCodeText).foregroundStyle(.green)
            }
        }
        .font(.caption2.weight(.medium).monospacedDigit())
        .fixedSize()
    }

    private var statusCodeText: String {
        log.upstreamStatus > 0 ? String(log.upstreamStatus) : "ERR"
    }

    private func chip(_ text: String, tint: Color) -> some View {
        Text(text)
            .font(.system(size: 9, weight: .bold))
            .padding(.horizontal, 4)
            .padding(.vertical, 1)
            .background(Capsule().fill(tint.opacity(0.18)))
            .foregroundStyle(tint)
            .fixedSize()
    }

    // MARK: - 行样式与悬停明细

    private var pendingTime: String {
        guard let at = log.loggedAt else { return "" }
        let secs = max(0, Int(Date().timeIntervalSince(at)))
        return secs < 60 ? "\(secs)s" : "\(secs / 60)m"
    }

    /// 行底:玻璃面内部用极浅的 primary 叠色划出行的范围,不额外套玻璃(那会再添一层硬边)。
    private var rowBackground: Color {
        log.isError ? Color.red.opacity(0.14) : Color.primary.opacity(0.06)
    }

    private var hoverHelp: String {
        var lines: [String] = []
        if !log.model.isEmpty { lines.append(log.model) }
        if !log.createdAt.isEmpty { lines.append(log.fullClockText) }

        var route: [String] = []
        if !log.protocolName.isEmpty { route.append("协议 \(log.protocolLabel)") }
        if !log.forwardMode.isEmpty { route.append("模式 \(log.modeLabel)") }
        route.append(log.stream ? "流式 SSE" : "非流式")
        lines.append(route.joined(separator: " · "))

        if !log.channelName.isEmpty { lines.append("渠道 \(log.channelName)") }
        if !log.keyName.isEmpty { lines.append("调用方 \(log.keyName)") }
        if !log.userAgent.isEmpty { lines.append("客户端 \(log.userAgent)") }
        if !log.clientIP.isEmpty { lines.append("IP \(log.clientIP)") }
        if !log.sessionID.isEmpty { lines.append("会话 \(log.sessionID)") }

        if !log.isPending {
            lines.append(
                "tokens 入 \(TokenFormatter.compact(log.promptTokens))"
                    + " / 出 \(TokenFormatter.compact(log.completionTokens))"
                    + " / 共 \(TokenFormatter.compact(log.totalTokens))"
            )
            lines.append(
                "缓存 cached \(TokenFormatter.compact(log.cachedTokens))"
                    + " / write \(TokenFormatter.compact(log.cacheWriteTokens))"
                    + " (\(TokenFormatter.percent(log.cacheHitRate)))"
            )
            var cost = "花费 \(log.costText(money: money))"
            if let style = log.usageStyle, !style.isEmpty { cost += " · 计价口径 \(style)" }
            if let period = pricePeriodLabel { cost += " · \(period)" }
            lines.append(cost)
            lines.append("状态 \(statusCodeText) · 耗时 \(TokenFormatter.duration(log.durationMS))")
        }
        if !log.error.isEmpty { lines.append("错误:\(log.error)") }
        return lines.joined(separator: "\n")
    }

    private var pricePeriodLabel: String? {
        switch log.pricePeriod {
        case "peak": "高峰时段"
        case "off_peak": "空闲时段(错峰价)"
        default: nil
        }
    }
}
