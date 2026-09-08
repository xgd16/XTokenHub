import SwiftUI

/// 实时请求流单行:状态图标 + 模型/来源 + tokens/耗时;错误行红色高亮。
struct LiveRequestRow: View {
    let log: RequestLog

    var body: some View {
        HStack(spacing: 9) {
            statusIcon
                .frame(width: 18)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 5) {
                    Text(log.model.isEmpty ? "未知模型" : log.model)
                        .font(.callout.weight(.medium))
                        .lineLimit(1)
                        .truncationMode(.middle)
                    if log.stream {
                        Text("流")
                            .font(.system(size: 9, weight: .bold))
                            .padding(.horizontal, 4)
                            .padding(.vertical, 1)
                            .background(Capsule().fill(Color.accentColor.opacity(0.18)))
                            .foregroundStyle(Color.accentColor)
                    }
                }
                Text(subtitle)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 2) {
                if log.isPending {
                    Text("生成中…")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(.orange)
                } else {
                    Text(TokenFormatter.compact(log.totalTokens))
                        .font(.callout.monospacedDigit())
                }
                Text(durationText)
                    .font(.caption2.monospacedDigit())
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: 8).fill(rowBackground))
        .help(hoverHelp)
    }

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

    private var subtitle: String {
        if !log.error.isEmpty { return log.error }
        var parts: [String] = []
        if !log.channelName.isEmpty { parts.append(log.channelName) }
        if !log.keyName.isEmpty { parts.append(log.keyName) }
        if parts.isEmpty { parts.append(log.protocolName) }
        return parts.joined(separator: " · ")
    }

    private var durationText: String {
        log.isPending ? pendingTime : TokenFormatter.duration(log.durationMS)
    }

    private var pendingTime: String {
        guard let at = log.loggedAt else { return "" }
        let secs = max(0, Int(Date().timeIntervalSince(at)))
        return secs < 60 ? "\(secs)s" : "\(secs / 60)m"
    }

    private var rowBackground: Color {
        log.isError ? Color.red.opacity(0.12) : Color.primary.opacity(0.045)
    }

    private var hoverHelp: String {
        var lines = [log.model]
        if !log.userAgent.isEmpty { lines.append(log.userAgent) }
        if !log.clientIP.isEmpty { lines.append(log.clientIP) }
        if !log.error.isEmpty { lines.append("错误:\(log.error)") }
        return lines.joined(separator: "\n")
    }
}
