import Foundation

/// WS 推送事件(信封 {type, payload, ts})的 Swift 侧表示。
enum HubEvent: Equatable, Sendable {
    case requestStarted(RequestLog)
    case requestCompleted(RequestLog)
    case statsUpdated
    case throughput(tokensPerSec: Double, activeStreams: Int)
    /// 渠道探测/启停变化,面板据此重拉渠道列表。
    case channelUpdated
    /// 渠道余额变化(后端拉取成功后推送),面板直接合并,无需等下一次轮询。
    case channelBalanceUpdated(ChannelBalancePayload)
    case ignored(String)
}

/// 解析服务端文本帧:先用 JSONSerialization 拆信封,再按 type 解码强类型载荷。
enum WsDecoder {
    static func decode(_ text: String) -> HubEvent? {
        guard let data = text.data(using: .utf8),
              let envelope = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = envelope["type"] as? String
        else { return nil }

        switch type {
        case "ping", "pong":
            return .ignored(type)
        case "stats.updated":
            return .statsUpdated
        case "stats.throughput":
            guard let payload = payloadData(envelope["payload"]),
                  let p = try? JSONDecoder().decode(ThroughputPayload.self, from: payload)
            else { return nil }
            return .throughput(tokensPerSec: p.tokensPerSec, activeStreams: p.activeStreams)
        case "request.started":
            guard let log = decodeLog(envelope["payload"]) else { return nil }
            return .requestStarted(log)
        case "request.completed":
            guard let log = decodeLog(envelope["payload"]) else { return nil }
            return .requestCompleted(log)
        case "channel.balance_updated":
            // 载荷畸形时降级为 channelUpdated,仍触发重拉,不丢刷新
            guard let payload = payloadData(envelope["payload"]),
                  let p = try? JSONDecoder().decode(ChannelBalancePayload.self, from: payload)
            else { return .channelUpdated }
            return .channelBalanceUpdated(p)
        case "channel.status_changed", "channel.probe_result":
            return .channelUpdated
        default:
            return .ignored(type)
        }
    }

    private static func decodeLog(_ any: Any?) -> RequestLog? {
        guard let data = payloadData(any) else { return nil }
        return try? JSONDecoder().decode(RequestLog.self, from: data)
    }

    private static func payloadData(_ any: Any?) -> Data? {
        guard let any, !(any is NSNull) else { return nil }
        return try? JSONSerialization.data(withJSONObject: any)
    }
}
