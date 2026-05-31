// swift-tools-version: 5.10
// 第八轮（E）：iOS App 骨架。
// 用 Swift Package Manager 组织代码；实际打包为 iOS App 时建议
// 1. 新建 Xcode Project（iOS App, SwiftUI, Swift），
// 2. File → Add Package Dependencies → Add Local → 选 clients/ios/YunmaoApp，
// 3. 把 `YunmaoApp` library 加到 App Target，App entry 使用 YunmaoApp/App.swift。

import PackageDescription

let package = Package(
    name: "YunmaoApp",
    platforms: [
        .iOS(.v16),
        .macOS(.v13), // 用于 XCTest / SPM 在 mac 上跑单测
    ],
    products: [
        .library(name: "YunmaoApp", targets: ["YunmaoApp"]),
    ],
    dependencies: [
        // WebRTC：使用 stasel/WebRTC（CocoaPods + SPM 双发布）
        .package(url: "https://github.com/stasel/WebRTC.git", from: "125.0.0"),
        // Keychain：轻量封装
        .package(url: "https://github.com/kishikawakatsumi/KeychainAccess.git", from: "4.2.2"),
    ],
    targets: [
        .target(
            name: "YunmaoApp",
            dependencies: [
                .product(name: "WebRTC", package: "WebRTC"),
                .product(name: "KeychainAccess", package: "KeychainAccess"),
            ],
            path: "Sources/YunmaoApp"
        ),
        .testTarget(
            name: "YunmaoAppTests",
            dependencies: ["YunmaoApp"],
            path: "Tests/YunmaoAppTests"
        ),
    ]
)
