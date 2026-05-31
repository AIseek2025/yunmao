// yunmao iOS App entry。
// 第八轮（E）：SwiftUI App，最低 iOS 16；接 WebRTC + StoreKit2 + Keychain。

import SwiftUI

// 注意：作为 SwiftPM 库，这里不能写 `@main`，否则会与 XCTest runner 的 `_main`
// 符号冲突。下游 Xcode 项目用 `@main struct YunmaoAppEntry: App { var body: some Scene { WindowGroup { ContentView().environmentObject(SessionStore()) } } }`
// 直接把本结构作为 root；package 暴露 SwiftUI View 即可。
public struct YunmaoApp: App {
    @StateObject private var session = SessionStore()

    public init() {}

    public var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(session)
        }
    }
}

public struct ContentView: View {
    @EnvironmentObject var session: SessionStore

    public init() {}

    public var body: some View {
        if session.isAuthenticated {
            TabRoot()
        } else {
            LoginView()
        }
    }
}

public struct TabRoot: View {
    public init() {}

    public var body: some View {
        TabView {
            RoomListView()
                .tabItem { Label("房间", systemImage: "tv") }
            ProfileView()
                .tabItem { Label("我的", systemImage: "person") }
        }
    }
}
