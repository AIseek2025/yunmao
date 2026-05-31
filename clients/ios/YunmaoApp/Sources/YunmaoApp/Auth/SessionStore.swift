// SessionStore：登录状态 + Keychain token 持久化。

import Foundation
#if canImport(KeychainAccess)
import KeychainAccess
#endif

@MainActor
public final class SessionStore: ObservableObject {
    @Published public private(set) var auth: AuthToken?
    @Published public private(set) var isAuthenticated: Bool = false

    public let api: YunmaoAPI

    private let keychainKey = "yunmao.auth.token"

    public init(baseURL: URL = URL(string: "https://api.yunmao.live")!) {
        let provider: () -> String? = {
            // 注意：actor isolation 需要返回当前缓存的 token；KeychainAccess 是 sync 的。
            #if canImport(KeychainAccess)
            let kc = Keychain(service: "live.yunmao.app")
            return kc["yunmao.auth.token"]
            #else
            return UserDefaults.standard.string(forKey: "yunmao.auth.token")
            #endif
        }
        self.api = YunmaoAPI(baseURL: baseURL, tokenProvider: provider)
        loadFromKeychain()
    }

    public func login(phone: String, code: String) async throws {
        let token = try await api.login(phone: phone, code: code)
        await persist(token: token)
    }

    public func logout() async {
        #if canImport(KeychainAccess)
        let kc = Keychain(service: "live.yunmao.app")
        try? kc.remove(keychainKey)
        #else
        UserDefaults.standard.removeObject(forKey: keychainKey)
        #endif
        auth = nil
        isAuthenticated = false
    }

    private func persist(token: AuthToken) async {
        #if canImport(KeychainAccess)
        let kc = Keychain(service: "live.yunmao.app")
        kc[keychainKey] = token.token
        kc["yunmao.auth.expires_at"] = String(token.expiresAt)
        kc["yunmao.auth.user_id"] = token.userId
        #else
        UserDefaults.standard.set(token.token, forKey: keychainKey)
        #endif
        auth = token
        isAuthenticated = !token.isExpired
    }

    private func loadFromKeychain() {
        #if canImport(KeychainAccess)
        let kc = Keychain(service: "live.yunmao.app")
        if let t = kc[keychainKey], let userID = kc["yunmao.auth.user_id"],
           let expStr = kc["yunmao.auth.expires_at"], let exp = TimeInterval(expStr) {
            auth = AuthToken(token: t, userId: userID, expiresAt: exp)
            isAuthenticated = !(auth?.isExpired ?? true)
        }
        #endif
    }
}
