// YunmaoAPI：URLSession + async/await 客户端，统一注入 JWT。
//
// 端点（与后端 chi 路由对齐）：
//   - POST  /api/v1/auth/login          → AuthToken
//   - GET   /api/v1/rooms               → [Room]
//   - GET   /api/v1/rooms/{id}/subscription → RoomSubscription
//   - GET   /v1/rooms/{id}/ice-servers  → IceServersResponse
//   - POST  /api/v1/feed                → FeedResponse
//   - GET   /api/v1/wallets/{user_id}   → Wallet
//   - POST  /api/v1/orders/{id}/prepay  → PrepayResponse
//   - POST  /api/v1/pay/webhook/appleiap → ack
//
// 错误：HTTPError（非 2xx）+ DecodingError + URLError。

import Foundation

public enum YunmaoAPIError: Error, LocalizedError {
    case http(Int, String)
    case noToken
    case invalidResponse

    public var errorDescription: String? {
        switch self {
        case .http(let code, let body): return "HTTP \(code): \(body)"
        case .noToken: return "missing auth token"
        case .invalidResponse: return "invalid response"
        }
    }
}

public actor YunmaoAPI {
    public let baseURL: URL
    private let session: URLSession
    private var tokenProvider: () -> String?

    public init(baseURL: URL, session: URLSession = .shared, tokenProvider: @escaping () -> String?) {
        self.baseURL = baseURL
        self.session = session
        self.tokenProvider = tokenProvider
    }

    public func updateTokenProvider(_ provider: @escaping () -> String?) {
        self.tokenProvider = provider
    }

    // MARK: - generic

    public func get<T: Decodable>(_ path: String, query: [URLQueryItem] = [], as type: T.Type = T.self) async throws -> T {
        let req = try request(path: path, method: "GET", query: query, body: nil)
        return try await execute(req)
    }

    public func post<B: Encodable, T: Decodable>(_ path: String, body: B, as type: T.Type = T.self) async throws -> T {
        let data = try JSONEncoder.yunmao.encode(body)
        var req = try request(path: path, method: "POST", body: data)
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        return try await execute(req)
    }

    public func postRaw(_ path: String, body: Data, contentType: String = "application/json") async throws -> Data {
        var req = try request(path: path, method: "POST", body: body)
        req.setValue(contentType, forHTTPHeaderField: "Content-Type")
        let (data, resp) = try await session.data(for: req)
        try assertOK(resp, body: data)
        return data
    }

    // MARK: - typed endpoints

    public func login(phone: String, code: String) async throws -> AuthToken {
        struct In: Codable { let phone: String; let code: String }
        return try await post("/api/v1/auth/login", body: In(phone: phone, code: code))
    }

    public func listRooms() async throws -> [Room] {
        struct Wrap: Codable { let rooms: [Room] }
        let w: Wrap = try await get("/api/v1/rooms")
        return w.rooms
    }

    public func subscription(roomID: String) async throws -> RoomSubscription {
        try await get("/api/v1/rooms/\(roomID)/subscription")
    }

    public func iceServers(roomID: String) async throws -> IceServersResponse {
        try await get("/v1/rooms/\(roomID)/ice-servers")
    }

    public func feed(_ req: FeedRequest) async throws -> FeedResponse {
        try await post("/api/v1/feed", body: req)
    }

    public func wallet(userID: String) async throws -> Wallet {
        try await get("/api/v1/wallets/\(userID)")
    }

    public func appleIAPWebhook(payload: Data) async throws {
        _ = try await postRaw("/api/v1/pay/webhook/appleiap", body: payload)
    }

    // MARK: - core

    private func request(path: String, method: String, query: [URLQueryItem] = [], body: Data?) throws -> URLRequest {
        var components = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { components.queryItems = query }
        guard let url = components.url else { throw YunmaoAPIError.invalidResponse }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.httpBody = body
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if let token = tokenProvider() {
            req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return req
    }

    private func execute<T: Decodable>(_ req: URLRequest) async throws -> T {
        let (data, resp) = try await session.data(for: req)
        try assertOK(resp, body: data)
        return try JSONDecoder.yunmao.decode(T.self, from: data)
    }

    private func assertOK(_ resp: URLResponse, body: Data) throws {
        guard let http = resp as? HTTPURLResponse else { throw YunmaoAPIError.invalidResponse }
        if !(200..<300).contains(http.statusCode) {
            throw YunmaoAPIError.http(http.statusCode, String(data: body, encoding: .utf8) ?? "")
        }
    }
}

extension JSONEncoder {
    public static let yunmao: JSONEncoder = {
        let e = JSONEncoder()
        e.keyEncodingStrategy = .convertToSnakeCase
        return e
    }()
}

extension JSONDecoder {
    public static let yunmao: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        return d
    }()
}
