# yunmao iOS App 骨架

> 第八轮（E）落地。SwiftUI + WebRTC + StoreKit2 + KeychainAccess。

## 工程结构

```
clients/ios/YunmaoApp/
├── Package.swift                # SPM 入口
├── Sources/YunmaoApp/
│   ├── App.swift                 # @main YunmaoApp + ContentView + TabRoot
│   ├── Models/Models.swift       # AuthToken / Room / FeedRequest / ChatMessage / ...
│   ├── Network/
│   │   ├── YunmaoAPI.swift       # URLSession + async/await + 统一 Bearer
│   │   └── WSClient.swift        # URLSessionWebSocketTask + AsyncStream<WSEvent>
│   ├── WebRTC/WhepClient.swift   # RTCPeerConnection + WHEP POST SDP + 渲染
│   ├── Payments/StoreKitManager.swift # StoreKit2 IAP + 后端 webhook 上报
│   ├── Auth/SessionStore.swift   # Keychain 持久化 + ObservableObject
│   ├── Util/GrayHit.swift        # FNV1a hash100，与后端 featureflags 等价
│   └── Views/
│       ├── LoginView.swift
│       ├── RoomListView.swift
│       ├── RoomDetailView.swift  # 播放 + 投喂 + 弹幕
│       └── ProfileView.swift     # 钱包 + IAP 充值
└── Tests/YunmaoAppTests/
    └── YunmaoAppTests.swift      # GrayHit 分布、Token、WS 解析单测
```

## 依赖

- `stasel/WebRTC` 125.0.0+：Google WebRTC pre-built XCFramework；
- `kishikawakatsumi/KeychainAccess` 4.2.2+：Keychain 操作。

## 打开方式

### 选项 A：Xcode 直接打开（推荐）

```bash
cd clients/ios/YunmaoApp
xed .
```

Xcode 会识别 Package.swift；选 `My Mac` 跑测试，或新建 iOS App target 把 YunmaoApp library 链入。

### 选项 B：纯 Xcode Project

1. 新建 Xcode iOS App（SwiftUI，最低 iOS 16，Bundle ID `live.yunmao.app`）
2. File → Add Package Dependencies → 本地 → 选 `clients/ios/YunmaoApp`
3. App entry 改为：
   ```swift
   import YunmaoApp
   @main struct App: SwiftUI.App {
       var body: some Scene { YunmaoApp().body }
   }
   ```

## 跑测试

```bash
cd clients/ios/YunmaoApp
swift test           # macOS 上跑非 UI 单测（GrayHit / Models / Token）
# 真机 / 模拟器：
xcodebuild -scheme YunmaoApp -destination 'platform=iOS Simulator,name=iPhone 16' test
```

## 主要联调点

- `SessionStore` 默认 baseURL = `https://api.yunmao.live`；dev 改为 `http://localhost:18000`。
- WS URL：`wss://api.yunmao.live/ws`；dev 改为 `ws://localhost:18007`（gateway-svc）。
- `WhepClient` 需要 iOS 上具备 WebRTC 静态库；通过 `stasel/WebRTC` 包自动接入。
- StoreKit2 测试：用 `Configuration.storekit` 文件（Apple 提供）配置三档产品 ID。
- Bundle ID：`live.yunmao.app`（与 billing-svc AppleIAP `AppleIAPConfig.BundleID` 一致）。

## 待业务确认

1. Apple App Apple ID（App Store Connect 数字 ID）；
2. 三档产品定价（com.yunmao.coin.{small,medium,large}）；
3. 是否启用 sandbox `StoreKit Configuration File`（推荐 dev / CI）；
4. App Store 上架资料（隐私政策、年龄分级）。
