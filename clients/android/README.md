# yunmao Android 客户端骨架

第八轮（任务 F）落地的 Android 客户端工程骨架，对应 iOS（任务 E）与 Web（任务 G）。

## 技术栈

- Kotlin 2.0 + AGP 8.5 + JDK 17
- Jetpack Compose (BOM 2024.09) + Navigation Compose
- 网络：Ktor 2.3 (`ktor-client-okhttp`) + kotlinx-serialization
- WebSocket：Ktor WebSockets（备选 OkHttp WebSocket）
- WebRTC：`io.github.webrtc-sdk:android:125.6422.06.1`
- 视频播放：Media3 ExoPlayer + HLS（LL-HLS）
- 支付：`com.tencent.mm.opensdk:wechat-sdk-android` + `com.alipay.sdk:alipaysdk-android`
- 持久化：DataStore Preferences（token）
- 图片：Coil
- 测试：JUnit4 / kotlinx-coroutines-test / ktor-client-mock

## 目录结构

```
clients/android/
├── settings.gradle.kts
├── build.gradle.kts
├── gradle.properties
├── app/
│   ├── build.gradle.kts
│   ├── proguard-rules.pro
│   └── src/main/
│       ├── AndroidManifest.xml
│       ├── java/live/yunmao/app/
│       │   ├── MainActivity.kt
│       │   ├── YunmaoApplication.kt
│       │   ├── model/Models.kt           # 后端 API DTO（与 iOS / Web 同源）
│       │   ├── network/YunmaoApi.kt       # Ktor HTTP 客户端，统一 JWT 注入
│       │   ├── network/WSClient.kt        # WebSocket 客户端，事件解析
│       │   ├── webrtc/WhepClient.kt       # WHEP 拉流
│       │   ├── pay/PayManager.kt          # 微信 / 支付宝拉起
│       │   ├── util/GrayHit.kt            # FNV1a 灰度命中（与后端一致）
│       │   └── ui/                        # Compose 页面
│       └── res/values/(strings|themes).xml
└── app/src/test/java/live/yunmao/app/    # JUnit 单测
```

## 构建步骤

```
# 安装 JDK 17 + Android Studio Koala/Iguana 以上
cd clients/android
./gradlew assembleDebug          # 编译 debug apk
./gradlew test                   # 跑单元测试
```

> 当前仓库不包含 `gradlew` wrapper jar（避免提交大二进制）。Android Studio 首次打开会自动生成；
> 或手动执行 `gradle wrapper --gradle-version 8.7`。

## 关键集成点

- **后端 base URL**：通过 `BuildConfig.API_BASE` / `BuildConfig.WS_BASE` 注入，debug 默认 `10.0.2.2:18000`（模拟器回环宿主机）。
- **微信 AppID**：`BuildConfig.WECHAT_APP_ID`，需在腾讯开放平台申请 + 包名签名指纹绑定。
- **支付宝 AppID + 商户私钥**：服务端 `billing-svc` 配置，客户端只负责拉起 `payV2(orderString)`。
- **WHEP / TURN ICE Servers**：通过 `GET /api/v1/rooms/{id}/ice-servers` 获取，注入 `PeerConnection.RTCConfiguration`。
- **JWT**：DataStore 存储；过期前 30s 由调用方主动刷新。
- **灰度**：`GrayHit.inGrayPercent(roomId, percent)` 与 Go/Rust/iOS 同算法。

## 本机未验证项（受工具链限制）

- `./gradlew assembleDebug` 未在本工作区运行（缺少 Android SDK / wrapper jar）。CI 流水线 `.github/workflows/android-it.yml`（下一轮接入）会以 `android-actions/setup-android@v3` 跑 `assembleDebug` + `test`。
- WeChat / 支付宝真实账号未配置；当前为 placeholder appId。
- WebRTC 真机回放需要与 media-edge 的 WHEP endpoint 联调。

## 待办（移交下一轮）

- 接入 `room-svc /v1/rooms/{id}/ice-servers` REST，提供 `IceServer` 列表给 `WhepClient`。
- ExoPlayer + LL-HLS 播放器封装为 Composable。
- WeChat / Alipay 回调统一通过 `WXPayEntryActivity` / `PayTask.callback` 路由到 `WalletViewModel`。
- 实现 `LoginViewModel` / `RoomListViewModel` / `RoomDetailViewModel`，串联 `YunmaoApi`。
