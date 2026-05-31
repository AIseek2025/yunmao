// WSClient：URLSessionWebSocketTask 封装，处理订阅房间 + feed.* / chat.* / room.chat.moderation
// 事件。事件通过 AsyncStream 暴露。
//
// Phase 10 增强：自动指数退避重连（`reconnect` / `autoReconnect`），覆盖网络抖动、
// 前后台切换场景。`subscribedRooms` 缓存订阅列表，重连后自动恢复订阅。

import Foundation

public enum WSEvent {
    case feedStateChange(roomId: String, payload: [String: Any])
    case chatMessage(ChatMessage)
    case chatModeration(messageId: String, action: String)
    case reconnected
    case disconnected(Error?)
    case unknown(String, [String: Any])
}

public actor WSClient {
    private let url: URL
    private var task: URLSessionWebSocketTask?
    private var token: String
    private var continuation: AsyncStream<WSEvent>.Continuation?
    private var subscribedRooms: [String] = []
    private var reconnecting = false
    public var autoReconnect: Bool
    public var baseBackoffSeconds: Double
    public var maxBackoffSeconds: Double

    public init(url: URL, token: String, autoReconnect: Bool = true,
                baseBackoffSeconds: Double = 1.0, maxBackoffSeconds: Double = 30.0) {
        self.url = url
        self.token = token
        self.autoReconnect = autoReconnect
        self.baseBackoffSeconds = baseBackoffSeconds
        self.maxBackoffSeconds = maxBackoffSeconds
    }

    public func events() -> AsyncStream<WSEvent> {
        AsyncStream { cont in
            self.continuation = cont
            cont.onTermination = { @Sendable _ in
                Task { await self.close() }
            }
        }
    }

    public func connect() async {
        var req = URLRequest(url: url)
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        task = URLSession.shared.webSocketTask(with: req)
        task?.resume()
        Task { await pump() }
    }

    public func reconnect() async {
        guard !reconnecting else { return }
        reconnecting = true
        defer { reconnecting = false }
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
        var delay = baseBackoffSeconds
        for attempt in 0..<5 {
            try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            await connect()
            let rooms = subscribedRooms
            for roomId in rooms {
                await subscribe(roomId: roomId)
            }
            continuation?.yield(.reconnected)
            return
        }
    }

    public func subscribe(roomId: String) async {
        if !subscribedRooms.contains(roomId) {
            subscribedRooms.append(roomId)
        }
        let msg: [String: Any] = ["type": "subscribe", "room_id": roomId]
        try? await sendJSON(msg)
    }

    public func sendChat(roomId: String, body: String, clientID: String) async throws {
        let msg: [String: Any] = [
            "type": "chat.send",
            "room_id": roomId,
            "body": body,
            "client_msg_id": clientID,
        ]
        try await sendJSON(msg)
    }

    public func close() async {
        autoReconnect = false
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
        continuation?.finish()
    }

    private func sendJSON(_ msg: [String: Any]) async throws {
        guard let task else { return }
        let data = try JSONSerialization.data(withJSONObject: msg)
        try await task.send(.data(data))
    }

    private func pump() async {
        guard let task else { return }
        while task.state == .running {
            do {
                let m = try await task.receive()
                switch m {
                case .string(let s):
                    handle(s.data(using: .utf8) ?? Data())
                case .data(let d):
                    handle(d)
                @unknown default: break
                }
            } catch {
                continuation?.yield(.disconnected(error))
                if autoReconnect {
                    Task { await self.reconnect() }
                } else {
                    continuation?.finish()
                }
                return
            }
        }
    }

    private func handle(_ data: Data) {
        guard let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        let type = (dict["type"] as? String) ?? "unknown"
        switch type {
        case "feed.state_change", "feed.created", "feed.completed":
            let roomId = (dict["room_id"] as? String) ?? ""
            continuation?.yield(.feedStateChange(roomId: roomId, payload: dict))
        case "room.chat.message", "chat.message":
            if let payload = dict["payload"] as? [String: Any],
               let json = try? JSONSerialization.data(withJSONObject: payload),
               let msg = try? JSONDecoder.yunmao.decode(ChatMessage.self, from: json) {
                continuation?.yield(.chatMessage(msg))
            }
        case "room.chat.moderation", "chat.moderation":
            let mid = (dict["message_id"] as? String) ?? ""
            let action = (dict["action"] as? String) ?? "hide"
            continuation?.yield(.chatModeration(messageId: mid, action: action))
        default:
            continuation?.yield(.unknown(type, dict))
        }
    }
}
