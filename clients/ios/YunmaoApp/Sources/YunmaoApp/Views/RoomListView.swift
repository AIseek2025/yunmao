import SwiftUI

public struct RoomListView: View {
    @EnvironmentObject var session: SessionStore
    @State private var rooms: [Room] = []
    @State private var loading: Bool = false
    @State private var error: String?

    public init() {}

    public var body: some View {
        NavigationStack {
            List(rooms) { room in
                NavigationLink(value: room) {
                    HStack {
                        VStack(alignment: .leading) {
                            Text(room.name).font(.headline)
                            Text(room.status).font(.caption).foregroundColor(.secondary)
                        }
                        Spacer()
                        if room.webrtcEligible == true {
                            Text("WebRTC").font(.caption2).padding(4).background(Color.green.opacity(0.2)).cornerRadius(4)
                        } else {
                            Text("LL-HLS").font(.caption2).padding(4).background(Color.blue.opacity(0.2)).cornerRadius(4)
                        }
                    }
                }
            }
            .refreshable { await reload() }
            .navigationTitle("直播房间")
            .navigationDestination(for: Room.self) { room in
                RoomDetailView(room: room)
            }
        }
        .task { await reload() }
    }

    private func reload() async {
        loading = true
        do {
            rooms = try await session.api.listRooms()
        } catch {
            self.error = String(describing: error)
        }
        loading = false
    }
}
