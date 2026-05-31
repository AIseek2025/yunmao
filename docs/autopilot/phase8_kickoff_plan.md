# Phase 8 Kickoff Plan — Phase 1 MVP Recovery

**Created**: Iteration 337  
**Phase**: 8 — Phase 1 MVP Recovery  
**Status**: planning_complete, ready_for_execution

---

## 1. Phase 7 Archive Confirmation

Phase 7 (Closeout Fresh Evidence And Release Readiness) has been **audit-approved** as of iteration 336:

| Track | Evidence | Tests | Status |
|-------|----------|-------|--------|
| Payments / IAP | `billing-svc` `/internal/diagnose/credentials` | 2 passed | ✅ Closed |
| TURN / RTC | `room-svc` `/v1/rooms/{id}/ice-servers` | 3 passed | ✅ Closed |
| JWT / JWKS | `room-svc` `/jwks.json` + `/internal/keys/health` | 2 passed | ✅ Closed |
| Rust baseline | 76 workspace tests, 0 clippy warnings | ✓ | ✅ Healthy |
| Go baseline | 33 packages, all passed | ✓ | ✅ Healthy |

Phase 7 is archived. No further work required.

---

## 2. Current Client Inventory

### Web (`clients/web/`) — Next.js App Router

| Page | Path | State | MVP Gap |
|------|------|-------|---------|
| Landing | `src/app/page.tsx` | Skeleton (links to /login, /rooms) | Minimal — functional |
| Login | `src/app/login/page.tsx` | Exists | Needs verification |
| Room List | `src/app/rooms/page.tsx` | Exists | Needs verification |
| Room Detail | `src/app/rooms/[id]/page.tsx` | Substantial (WHEP + HLS + WS + feed + chat) | Mostly complete |
| Profile | `src/app/me/page.tsx` | Substantial (wallet, logout) | Mostly complete |
| Wallet | `src/app/me/wallet/page.tsx` | Exists | Needs verification |

### Android (`clients/android/`) — Jetpack Compose

| Screen | Path | State | MVP Gap |
|--------|------|-------|---------|
| Entry | `MainActivity.kt` | Minimal (sets up NavGraph) | OK |
| Navigation | `ui/NavGraph.kt` | 4 routes: login→rooms→room/{id}→me | OK |
| Screens | `ui/Screens.kt` | 4 screens | Needs verification |
| API | `network/YunmaoApi.kt` | Exists | Needs verification |
| WebSocket | `network/WSClient.kt` | Exists | Needs verification |
| WHEP | `webrtc/WhepClient.kt` | Exists | Needs verification |
| Payment | `pay/PayManager.kt` | Exists | Needs verification |

### iOS (`clients/ios/`) — SwiftUI

| View | Path | State | MVP Gap |
|------|------|-------|---------|
| Entry | `App.swift` | SwiftUI App entry | OK |
| Room List | `Views/RoomListView.swift` | Functional (fetches rooms, navigation) | Working |
| Room Detail | `Views/RoomDetailView.swift` | Substantial (feed, chat, WS, playback) | Mostly complete |
| Login | `Views/LoginView.swift` | Exists | Needs verification |
| Profile | `Views/ProfileView.swift` | Exists | Needs verification |
| API | `Network/YunmaoAPI.swift` | Exists | Needs verification |
| WebSocket | `Network/WSClient.swift` | Exists | Needs verification |
| WHEP | `WebRTC/WhepClient.swift` | Exists | Needs verification |
| Payment | `Payments/StoreKitManager.swift` | Exists | Needs verification |
| Auth | `Auth/SessionStore.swift` | Exists | Needs verification |

---

## 3. Phase 8 Gap Analysis

### What exists

- **All three clients have MVP skeleton pages/views**: login, room list, room detail, profile
- **Room detail is the most fleshed out**: video playback (WHEP → HLS fallback), WebSocket chat, feed button, moderation handling
- **API clients exist** for all three platforms (Web, Android, iOS)
- **WebSocket clients exist** for all three platforms
- **WHEP/WebRTC clients exist** for all three platforms
- **Payment stubs exist** for all three platforms (PayManager, StoreKitManager, wallet page)

### What is missing or needs verification

1. **Cross-endpoint consistency**: The 3 clients may have drifted in API contracts (field names, error handling, auth flow). Need to verify alignment.

2. **No MVP smoke runbook** (`docs/dev/runbooks/mvp-smoke.md`): Phase 8 explicitly requires "at least one repeatable MVP verification entry or runbook".

3. **Deep-link / notification handling**: Phase 8 mentions "通知/深链/状态一致性缺口". No evidence of deep-link routing or push notification handling in any client.

4. **Weak-network / foreground-background recovery**: No evidence of reconnection logic, offline queuing, or session persistence across app lifecycle.

5. **State consistency**: Feed status, chat messages, and wallet balance may not sync correctly across clients viewing the same room.

6. **Login flow completion**: Need to verify token refresh, error handling, and session persistence across all clients.

---

## 4. Phase 8 Execution Strategy

### First Sprint Scope (Iteration 338+)

Priority 1 — **MVP Smoke Runbook** (exit criterion):
- Create `docs/dev/runbooks/mvp-smoke.md` with step-by-step verification for:
  - Login flow (all 3 clients)
  - Room list load (all 3 clients)
  - Room detail: video playback, chat, feed (all 3 clients)
  - Profile / wallet view (all 3 clients)
- Include curl equivalents for API validation
- Mark each step as ✅/❌ per client

Priority 2 — **Cross-client API contract verification**:
- Diff the API client types across Web (`@/lib/api`), Android (`YunmaoApi.kt`), iOS (`YunmaoAPI.swift`)
- Verify: login request/response shapes match
- Verify: subscription/ice-servers response shapes match
- Verify: feed request/response shapes match
- Document any drift in the runbook

Priority 3 — **Minimum deep-link / notification stub** (stretch):
- Add deep-link route for room detail in Android (intent filter) and iOS (universal link)
- Stub notification handler for feed.state_change events
- Document as explicit blocker if not achievable without real infrastructure

### Subsequent Sprints

- Login flow hardening (token refresh, error states)
- Weak-network reconnection logic
- Foreground/background session recovery
- Cross-client state consistency testing

---

## 5. Exit Criteria Checklist

- [ ] At least one Web/App MVP critical path has cross-endpoint consistency evidence
- [ ] At least one repeatable MVP smoke runbook exists (`docs/dev/runbooks/mvp-smoke.md`)
- [ ] Cross-client API contract alignment documented
- [ ] Deep-link/notification gaps explicitly catalogued (not masked)
- [ ] All MVP smoke steps executable or marked as external blockers

---

## 6. Required Prerequisites

- Backend services running (Go services: user-svc, room-svc, feeding-svc, billing-svc, chat-svc)
- Rust media services running (gateway, ingest, media-edge)
- Redis accessible
- PostgreSQL with migrations applied
- EMQX (MQTT broker) — optional for chat testing

---

## 7. Owner Judgment Alignment

This plan aligns with the owner takeover protocol:
- `owner_action = request_planning` → **This document satisfies the request**
- `takeover_mode = owner_replan` → **Phase 8 replan is now materialized**
- `owner_judgment = "当前已定义 phases 已全部完成；继续推进前需先做归档或下一 phase 规划"` → **Phase 7 archived above; Phase 8 scoped with concrete first steps**
- `restart_strategy = pause_relaunch_until_replan_is_materialized` → **Replan now materialized; next iteration can proceed to coding**

The next iteration (338+) should begin executing Priority 1 (MVP smoke runbook) since it is a low-risk, high-value task that immediately produces audit-visible artifacts.
