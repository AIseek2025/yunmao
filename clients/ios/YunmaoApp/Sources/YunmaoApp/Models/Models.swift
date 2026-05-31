// 后端 API 数据模型。
// 与 services/{user,room,feeding,billing,chat}-svc 的 JSON schema 对齐。

import Foundation

public struct AuthToken: Codable, Equatable {
    public let token: String
    public let userId: String
    public let expiresAt: TimeInterval

    public init(token: String, userId: String, expiresAt: TimeInterval) {
        self.token = token
        self.userId = userId
        self.expiresAt = expiresAt
    }

    public var isExpired: Bool { Date().timeIntervalSince1970 > expiresAt - 30 }
}

public struct Room: Codable, Identifiable, Equatable, Hashable {
    public let id: String
    public let name: String
    public let status: String
    public let cover: String?
    public let protocolPref: String? // "ll-hls" | "webrtc"
    public let webrtcEligible: Bool?
}

public struct RoomSubscription: Codable {
    public let roomId: String
    public let token: String
    public let urlPlayback: String
    public let urlWhep: String?
    public let webrtcEnabled: Bool
}

public struct FeedRequest: Codable {
    public let roomId: String
    public let userId: String
    public let grams: Int
    public let feedTicketId: String
    public let idempotencyKey: String
}

public struct FeedResponse: Codable {
    public let id: String
    public let status: String // pending/dispensing/completed/failed
    public let camRecordUrl: String?
}

public struct Wallet: Codable {
    public let userId: String
    public let balanceFen: Int
    public let coins: Int
}

public struct ChatMessage: Codable, Identifiable {
    public let id: String
    public let roomId: String
    public let userId: String
    public let nickname: String?
    public let body: String
    public let createdAt: TimeInterval
    public var moderation: String? // 第七轮：pass/hide/warn/recall/block
}

public struct PrepayResponse: Codable {
    public let channel: String
    public let prepayId: String
    public let payUrl: String?
    public let qrContent: String?
    public let clientHints: [String: String]?
}

public struct IceServersResponse: Codable {
    public let iceServers: [IceServer]

    public struct IceServer: Codable {
        public let urls: [String]
        public let username: String?
        public let credential: String?
    }
}
