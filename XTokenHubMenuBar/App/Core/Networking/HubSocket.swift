import Foundation

/// XTokenHub WebSocket 长连接:
/// - 文本心跳 {"type":"ping"}(服务端回 pong),先立即探测一次以尽快确认链路;
/// - 指数退避自动重连(1s→30s),成功收包后归零;
/// - 65s 无任何入站帧判死并强制重连。
/// 所有可变状态只在主线程读写;标记 @unchecked Sendable 仅为了让
/// URLSession/Task 闭包可以安全捕获 self,并发安全仍由 @MainActor 保证。
@MainActor
final class HubSocket: @unchecked Sendable {
    enum State: Equatable {
        case connecting
        case connected
        case failed(String)

        var isOnline: Bool { self == .connected }
    }

    private(set) var state: State = .connecting
    /// 业务事件回调(主线程)。
    var onEvent: ((HubEvent) -> Void)?
    /// 首个入站包确认链路可用后触发,用于全量补拉。
    var onConnected: (() -> Void)?

    private let url: URL
    private var session: URLSession?
    private var task: URLSessionWebSocketTask?
    private var receiveTask: Task<Void, Never>?
    private var heartbeatTask: Task<Void, Never>?
    private var watchdogTask: Task<Void, Never>?
    private var reconnectTask: Task<Void, Never>?
    private var attempt = 0
    private var lastInbound = Date.distantPast

    init(url: URL) {
        self.url = url
    }

    deinit {
        receiveTask?.cancel()
        heartbeatTask?.cancel()
        watchdogTask?.cancel()
        reconnectTask?.cancel()
        session?.invalidateAndCancel()
    }

    // MARK: - 生命周期

    func connect() {
        cancelReconnect()
        teardownConnection()
        setState(.connecting)

        let s = session ?? makeSession()
        session = s
        let t = s.webSocketTask(with: url)
        task = t
        t.resume()
        lastInbound = Date()

        receiveTask = Task { [weak self] in await self?.receiveLoop() }
        heartbeatTask = Task { [weak self] in await self?.heartbeatLoop() }
        watchdogTask = Task { [weak self] in await self?.watchdogLoop() }
    }

    func disconnect() {
        cancelReconnect()
        teardownConnection()
        session?.invalidateAndCancel()
        session = nil
        setState(.failed("已断开"))
    }

    // MARK: - 收发

    private func receiveLoop() async {
        while !Task.isCancelled, let task = self.task {
            do {
                let message = try await task.receive()
                handleMessage(message)
            } catch {
                scheduleReconnect("连接中断:\(shortError(error))")
                return
            }
        }
    }

    private func handleMessage(_ message: URLSessionWebSocketTask.Message) {
        lastInbound = Date()
        attempt = 0
        if state != .connected {
            setState(.connected)
            onConnected?()
        }
        if case .string(let text) = message, let event = WsDecoder.decode(text) {
            onEvent?(event)
        }
    }

    private func heartbeatLoop() async {
        // 立即探测一次,让"连接中"状态尽快收敛为已连接(静默 hub 也能确认链路)。
        try? await Task.sleep(for: .seconds(0.5))
        await sendPing()
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(25))
            guard !Task.isCancelled else { return }
            await sendPing()
        }
    }

    private func sendPing() async {
        guard let task else { return }
        do {
            try await task.send(.string(#"{"type":"ping"}"#))
        } catch {
            scheduleReconnect("发送心跳失败:\(shortError(error))")
        }
    }

    private func watchdogLoop() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(10))
            guard !Task.isCancelled else { return }
            if Date().timeIntervalSince(lastInbound) > 65 {
                scheduleReconnect("连接超时")
                return
            }
        }
    }

    // MARK: - 重连

    private func scheduleReconnect(_ reason: String) {
        guard reconnectTask == nil else { return }
        teardownConnection()
        setState(.failed(reason))
        let delay = min(30.0, pow(2.0, Double(min(attempt, 5))))
        attempt += 1
        reconnectTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(delay))
            guard let self, !Task.isCancelled else { return }
            self.reconnectTask = nil
            self.connect()
        }
    }

    private func cancelReconnect() {
        reconnectTask?.cancel()
        reconnectTask = nil
    }

    private func teardownConnection() {
        receiveTask?.cancel()
        receiveTask = nil
        heartbeatTask?.cancel()
        heartbeatTask = nil
        watchdogTask?.cancel()
        watchdogTask = nil
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
    }

    private func makeSession() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 15
        return URLSession(configuration: config)
    }

    private func setState(_ s: State) {
        guard state != s else { return }
        state = s
    }

    private func shortError(_ error: Error) -> String {
        (error as? URLError)?.localizedDescription ?? error.localizedDescription
    }
}
