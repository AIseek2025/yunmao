# MVP Smoke Runbook

## Purpose

Provides a repeatable entry point for validating the Phase 1 MVP core paths across Web, iOS, and Android clients against the Go + Rust backend.

## Prerequisites

1. Go services running locally (see `deploy/docker-compose.app.yml`):
   - `user-svc :8101`
   - `room-svc :8102`
   - `billing-svc :8104`
   - `feeding-svc :8103`
   - `chat-svc :8105`
2. Rust data-plane running locally:
   - `yunmao-gateway :18090`
   - `yunmao-media-edge :8080`
   - `yunmao-ingest :1935`
3. Web client: `pnpm dev` from `clients/web/`

## Web MVP Smoke (Primary)

### 1. Landing Page

```bash
open http://localhost:3000
```

- Verify: "yunmao 云养猫" header, "登录" and "浏览直播" buttons present
- "浏览直播" → navigates to `/rooms`
- "登录" → navigates to `/login`

### 2. Login Flow

```bash
open http://localhost:3000/login
```

- Enter phone number (any E.164 format for local dev, e.g. `+8613800138000`)
- Enter verification code (local dev: `123456` or whatever the mock SMS returns)
- Click "登录"
- Expected: redirects to `/rooms`

### 3. Room List

```bash
open http://localhost:3000/rooms
```

- Expected: API call to `/api/v1/rooms` succeeds
- Displays room cards with name, id, and protocol badge (WebRTC / LL-HLS)
- "进入直播 →" link navigates to `/rooms/{id}`
- Top-right: "我的" link → `/me`

### 4. Room Detail (Core MVP Path)

```bash
open http://localhost:3000/rooms/room_demo
```

- **Video Player**: HLS or WHEP stream loads (black screen if no ingest source is running, but `<video>` element present)
- **Feed Button**: "投喂 5g" button → calls `/api/v1/feed` → status updates to "dispensing" → "completed"
- **Chat (弹幕)**:
  - Type message in input field
  - Click "发送" → message appears in chat immediately (optimistic)
  - WebSocket broadcasts to other subscribers
  - `chat.moderation` events update message visibility
- **Navigation**: "← 房间列表" → `/rooms`, "我的" → `/me`

### 5. Personal Center

```bash
open http://localhost:3000/me
```

- Displays: user ID (from JWT)
- Displays: wallet balance from `/api/v1/wallets/{user_id}`
- "充值 →" → `/me/wallet`
- "退出登录" → clears session, redirects to `/login`
- "直播房间" → `/rooms`

## Automated Tests

```bash
cd clients/web
pnpm test:run
```

Expected: 14 tests pass (contract, ws, playback, gray)

```bash
cd rust/
cargo test --workspace
```

Expected: 76 tests pass

```bash
cd go/services/billing-svc && go test ./...
cd go/services/room-svc && go test ./...
cd go/services/user-svc && go test ./...
```

Expected: all pass

## Cross-Platform Consistency Matrix

| Feature | Web (Next.js) | iOS (SwiftUI) | Android (Compose) |
|---------|:---:|:---:|:---:|
| Login (phone + code) | `login/page.tsx` | `LoginView.swift` | `LoginScreen` (skeleton) |
| Room List | `rooms/page.tsx` | `RoomListView.swift` | `RoomListScreen` (hardcoded) |
| Room Detail + Video | `rooms/[id]/page.tsx` | `RoomDetailView.swift` | `RoomDetailScreen` (placeholder) |
| Feed (投喂) | API + WS state change | API + WS state change | Placeholder button |
| Chat (弹幕) | WS send + receive | WS send + receive | Placeholder text |
| Personal Center | `me/page.tsx` | `ProfileView.swift` | `ProfileScreen` (hardcoded) |
| Wallet | `me/wallet/page.tsx` | — | — |
| WHEP Playback | `lib/playback.ts` | `WhepClient.swift` | — |
| HLS Fallback | `hls.js` | AVPlayer | — |
| WebSocket | `lib/ws.ts` | `WSClient.swift` | — |
| Session/Auth | `lib/session.ts` (zustand) | `SessionStore.swift` | — |

### Cross-Platform Status

- **Web**: Full MVP with real API integration, WebSocket chat, WHEP/HLS playback, auth session
- **iOS**: Core MVP with real API, WebSocket chat, feed. Video player area is placeholder text (needs RTCMTLVideoView or AVPlayer integration)
- **Android**: Skeleton only — no real API calls, no WebSocket, no playback. Requires `YunmaoApi.kt` client layer and ViewModel integration

## Blockers

| Platform | Blocker | Exit Condition |
|----------|---------|----------------|
| Web | None for MVP core path | — |
| iOS | Video playback placeholder | Integrate AVPlayer for HLS or RTCMTLVideoView for WebRTC |
| Android | No API client, no WS, no video | Implement `YunmaoApi.kt`, `WSClient.kt`, ExoPlayer integration |
| All | JWT/JWKS cross-service auth | Wire `YUNMAO_JWKS_ENDPOINTS` in staging |
| All | TURN not provisioned | Inject `YUNMAO_TURN_SHARED_SECRET` |
