import Foundation
import Network

/// 只读的本机 HTTP 端点，把主 App 当前的数据交给 Widget。
///
/// 背景：本项目 ad-hoc 签名，无法使用 App Groups 共享容器
/// （见 `WidgetDataProvider` 顶部说明），所以改由主 App 在回环地址上
/// 暴露 `GET /snapshot`，Widget 从这里取数。
///
/// 安全约束：
/// - 只绑定 127.0.0.1，不对局域网暴露；
/// - 只实现 GET /snapshot，不接收任何请求体，不提供写操作；
/// - 响应内容仅是统计数据（不含 api_key 等敏感字段，`Channel` 模型本身也不解码密钥）。
final class WidgetBridgeServer: @unchecked Sendable {
    private let queue = DispatchQueue(label: "com.xtokenhub.menubar.widget-bridge")
    private let lock = NSLock()
    private var listener: NWListener?
    private var payloadData: Data?

    /// 启动监听；失败时静默（Widget 会退回到自己请求 XTokenHub 接口）。
    func start() {
        guard listener == nil else { return }

        let params = NWParameters.tcp
        params.allowLocalEndpointReuse = true
        guard let port = NWEndpoint.Port(rawValue: WidgetBridge.port) else { return }
        // 只绑定回环地址
        params.requiredLocalEndpoint = .hostPort(host: .ipv4(.loopback), port: port)

        do {
            let listener = try NWListener(using: params)
            listener.newConnectionHandler = { [weak self] connection in
                self?.handle(connection)
            }
            listener.stateUpdateHandler = { state in
                if case .failed = state {
                    // 端口被占用等情况：放弃桥接，不影响主功能
                    self.lock.lock()
                    self.listener?.cancel()
                    self.listener = nil
                    self.lock.unlock()
                }
            }
            listener.start(queue: queue)
            self.listener = listener
        } catch {
            listener = nil
        }
    }

    func stop() {
        lock.lock()
        listener?.cancel()
        listener = nil
        lock.unlock()
    }

    /// 更新待下发载荷（主 App 每次刷新数据后调用）。
    func update(_ payload: WidgetBridgePayload) {
        guard let data = try? JSONEncoder().encode(payload) else { return }
        lock.lock()
        payloadData = data
        lock.unlock()
    }

    // MARK: - 私有

    private func handle(_ connection: NWConnection) {
        connection.start(queue: queue)
        receiveRequest(connection, buffer: Data())
    }

    /// 读请求头（到 `\r\n\r\n` 为止）后立即回包，不读请求体。
    private func receiveRequest(_ connection: NWConnection, buffer: Data) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 8192) { [weak self] data, _, isComplete, error in
            guard let self else { return }

            var buffer = buffer
            if let data { buffer.append(data) }

            if error != nil {
                connection.cancel()
                return
            }

            guard let headerEnd = buffer.range(of: Data("\r\n\r\n".utf8)) else {
                if isComplete {
                    connection.cancel()
                } else {
                    self.receiveRequest(connection, buffer: buffer)
                }
                return
            }

            let head = String(decoding: buffer[..<headerEnd.lowerBound], as: UTF8.self)
            let path = Self.requestPath(from: head)
            self.respond(connection, path: path)
        }
    }

    private static func requestPath(from head: String) -> String {
        guard let requestLine = head.split(separator: "\r\n").first else { return "" }
        let parts = requestLine.split(separator: " ")
        guard parts.count >= 2 else { return "" }
        return String(parts[1])
    }

    private func respond(_ connection: NWConnection, path: String) {
        lock.lock()
        let body = payloadData
        lock.unlock()

        let status: String
        let payload: Data
        if path == WidgetBridge.snapshotPath, let body {
            status = "200 OK"
            payload = body
        } else if path == WidgetBridge.snapshotPath {
            status = "503 Service Unavailable"
            payload = Data(#"{"error":"no data yet"}"#.utf8)
        } else {
            status = "404 Not Found"
            payload = Data(#"{"error":"not found"}"#.utf8)
        }

        var response = Data()
        response.append(Data("HTTP/1.1 \(status)\r\n".utf8))
        response.append(Data("Content-Type: application/json; charset=utf-8\r\n".utf8))
        response.append(Data("Content-Length: \(payload.count)\r\n".utf8))
        response.append(Data("Connection: close\r\n\r\n".utf8))
        response.append(payload)

        connection.send(content: response, completion: .contentProcessed { _ in
            connection.cancel()
        })
    }
}
