import Foundation

/// 把用户输入的 Hub 地址规范化为 HTTP 基址与 WebSocket 地址。
enum HubURL {
    static let fallback = URL(string: "http://127.0.0.1:9192")!

    /// "  127.0.0.1:9192/ " → "http://127.0.0.1:9192";缺省协议按 http 处理。
    static func httpBase(_ raw: String) -> URL {
        var s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !s.isEmpty else { return fallback }
        let lower = s.lowercased()
        if !lower.hasPrefix("http://"), !lower.hasPrefix("https://") {
            s = "http://" + s
        }
        while s.hasSuffix("/") { s.removeLast() }
        guard let url = URL(string: s), url.host != nil else { return fallback }
        return url
    }

    /// http → ws、https → wss,追加管理端推送路径 /api/v1/ws。
    static func wsBase(_ http: URL) -> URL {
        let scheme = http.scheme?.lowercased() == "https" ? "wss" : "ws"
        var s = "\(scheme)://\(http.host ?? "127.0.0.1")"
        if let port = http.port { s += ":\(port)" }
        s += "/api/v1/ws"
        return URL(string: s) ?? fallback
    }
}
