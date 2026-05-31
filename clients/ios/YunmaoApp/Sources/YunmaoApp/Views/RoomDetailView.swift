import SwiftUI

public struct RoomDetailView: View {
    public let room: Room
    @EnvironmentObject var session: SessionStore
    @State private var subscription: RoomSubscription?
    @State private var feedStatus: String = "idle"
    @State private var chatMessages: [ChatMessage] = []
    @State private var chatInput: String = ""
    @State private var error: String?
    @State private var wsClient: WSClient?

    public init(room: Room) { self.room = room }

    public var body: some View {
        VStack {
            playerArea
                .frame(height: 220)
                .background(Color.black)
                .cornerRadius(8)
            HStack {
                Button(action: feed) {
                    Text("投喂 5g")
                        .padding(.horizontal, 16).padding(.vertical, 10)
                        .background(Color.accentColor).foregroundColor(.white)
                        .cornerRadius(8)
                }
                Text("状态：\(feedStatus)").font(.caption)
                Spacer()
            }
            .padding(.horizontal)
            Divider()
            ChatPanel(messages: $chatMessages, input: $chatInput) {
                Task { await sendChat() }
            }
            if let error { Text(error).foregroundColor(.red).font(.footnote) }
        }
        .padding()
        .navigationTitle(room.name)
        .task { await connect() }
        .onDisappear { Task { await wsClient?.close() } }
    }

    @ViewBuilder
    private var playerArea: some View {
        if subscription?.webrtcEnabled == true {
            Text("WebRTC 拉流（实际渲染需 RTCMTLVideoView）")
                .foregroundColor(.white)
        } else {
            Text("LL-HLS 拉流（AVPlayer 接管）")
                .foregroundColor(.white)
        }
    }

    private func connect() async {
        do {
            let sub = try await session.api.subscription(roomID: room.id)
            self.subscription = sub
            // WS：把 token 拼接到 ?token=... 或 Authorization
            guard let url = URL(string: "wss://api.yunmao.live/ws") else { return }
            let ws = WSClient(url: url, token: sub.token)
            self.wsClient = ws
            await ws.connect()
            await ws.subscribe(roomId: room.id)
            for await ev in await ws.events() {
                switch ev {
                case .feedStateChange(_, let payload):
                    feedStatus = (payload["status"] as? String) ?? feedStatus
                case .chatMessage(let m):
                    chatMessages.append(m)
                case .chatModeration(let mid, let action):
                    if let idx = chatMessages.firstIndex(where: { $0.id == mid }) {
                        if action == "recall" || action == "block" {
                            chatMessages.remove(at: idx)
                        } else {
                            chatMessages[idx].moderation = action
                        }
                    }
                case .unknown: break
                }
            }
        } catch {
            self.error = String(describing: error)
        }
    }

    private func feed() {
        guard let auth = session.auth else { return }
        Task {
            do {
                let req = FeedRequest(
                    roomId: room.id, userId: auth.userId,
                    grams: 5, feedTicketId: UUID().uuidString,
                    idempotencyKey: UUID().uuidString)
                let resp = try await session.api.feed(req)
                feedStatus = resp.status
            } catch {
                self.error = String(describing: error)
            }
        }
    }

    private func sendChat() async {
        let body = chatInput.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty else { return }
        chatInput = ""
        do {
            try await wsClient?.sendChat(roomId: room.id, body: body, clientID: UUID().uuidString)
        } catch {
            self.error = String(describing: error)
        }
    }
}

public struct ChatPanel: View {
    @Binding var messages: [ChatMessage]
    @Binding var input: String
    let onSend: () -> Void

    public var body: some View {
        VStack {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading) {
                        ForEach(messages) { m in
                            HStack {
                                Text(m.nickname ?? m.userId).bold().font(.caption)
                                Text(m.moderation == "hide" ? "***" : m.body)
                                    .font(.body)
                                    .opacity(m.moderation == "warn" ? 0.6 : 1.0)
                                Spacer()
                            }.id(m.id)
                        }
                    }
                    .padding(.horizontal)
                }
                .onChange(of: messages.count) { _ in
                    proxy.scrollTo(messages.last?.id, anchor: .bottom)
                }
            }
            HStack {
                TextField("说点什么…", text: $input).textFieldStyle(.roundedBorder)
                Button("发送", action: onSend).disabled(input.isEmpty)
            }
            .padding(.horizontal)
        }
    }
}
