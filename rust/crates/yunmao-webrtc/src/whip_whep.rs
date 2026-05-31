//! WHIP / WHEP HTTP MVP（第五轮）。
//!
//! 实现 RFC9725（WHIP）与 RFC9726（WHEP）的最小可联调形态：
//!
//! - `POST /whip/{room_id}`：发布端推送 SDP offer，平台返回 SDP answer +
//!   `Location: /whip/{room_id}/sessions/{sid}`，对应 [`Publisher`]；
//! - `POST /whep/{room_id}`：订阅端拉流 offer，平台返回 answer +
//!   `Location: /whep/{room_id}/sessions/{sid}`，对应 [`Subscriber`]；
//! - `DELETE /whip/.../sessions/{sid}` / `DELETE /whep/.../sessions/{sid}`：关闭会话；
//! - `Authorization: Bearer <room_subscription_token>`：复用 room-svc 签发的房间订阅 token，
//!   由调用者注入 [`SessionAuthenticator`] 完成解析（生产中走 JWKS）。
//!
//! 真正的 RTP/SRTP/DTLS 链路依赖 `webrtc-rs` / `str0m`，第五轮在 ADR-0016/0017 留 TODO；
//! 本实现：
//! - 接受任何 `kind=="offer"` 的 SDP，生成 stub answer（带 v=/o=/s=/t=/m= 字段，hls.js
//!   / 浏览器在 dev 模式下能完成信令握手但不会真正解码音视频）；
//! - 走完整 [`Signaling`] 接口，未来切实库时只替换 [`LocalSignaling`]。

use std::sync::Arc;

use axum::{
    body::Body,
    extract::{Path, State},
    http::{header, HeaderMap, HeaderValue, StatusCode},
    response::{IntoResponse, Response},
    routing::{delete, post},
    Router,
};

use crate::{IceServers, RtpRoomHub, SessionDescription, Signaling, WebRtcError};

/// 信令路由配置。
pub struct WhipWhepConfig {
    /// 路径前缀，默认 `/`（路由变成 `/whip/{room}` `/whep/{room}`）。
    pub prefix: String,
    /// Optional：房间订阅 token 校验。None 表示 dev 模式不校验。
    pub authenticator: Option<Arc<dyn SessionAuthenticator>>,
    /// Optional：RTP 扇出 hub。Some 时，WHIP 收到 SDP 后会从 SDP 中识别 codec，
    /// 同时把 RTP packetize 的输出路由给 WHEP 订阅者。dev 路径下，
    /// 真实 RTP/SRTP 链路仍是 stub（见 ADR-0018），但 packetize/扇出已真做。
    pub rtp_hub: Option<RtpRoomHub>,
    /// ICE 服务器配置（提供给客户端的 SDP answer 元数据）。
    pub ice: IceServers,
}

impl Default for WhipWhepConfig {
    fn default() -> Self {
        Self {
            prefix: "/".into(),
            authenticator: None,
            rtp_hub: None,
            ice: IceServers::default(),
        }
    }
}

/// 房间订阅 token 解析器；生产由 user-svc / room-svc 提供 JWKSClient 实现。
#[async_trait::async_trait]
pub trait SessionAuthenticator: Send + Sync {
    /// 校验 Bearer token；返回 user_id（或 audience）。
    async fn authenticate(&self, room_id: &str, bearer: &str) -> Result<String, WebRtcError>;
}

/// State 透传给 axum handler。
#[derive(Clone)]
struct AppState {
    sig: Arc<dyn Signaling>,
    auth: Option<Arc<dyn SessionAuthenticator>>,
    rtp_hub: Option<RtpRoomHub>,
    ice: IceServers,
}

/// 构造一个独立 axum Router 提供 WHIP/WHEP 端点。
pub fn router(sig: Arc<dyn Signaling>, cfg: WhipWhepConfig) -> Router {
    let prefix = if cfg.prefix == "/" {
        "".to_string()
    } else {
        cfg.prefix.trim_end_matches('/').to_string()
    };
    let state = AppState {
        sig,
        auth: cfg.authenticator,
        rtp_hub: cfg.rtp_hub,
        ice: cfg.ice,
    };
    Router::new()
        .route(&format!("{}/whip/:room_id", prefix), post(handle_whip))
        .route(&format!("{}/whep/:room_id", prefix), post(handle_whep))
        .route(
            &format!("{}/whip/:room_id/sessions/:sid", prefix),
            delete(handle_whip_delete),
        )
        .route(
            &format!("{}/whep/:room_id/sessions/:sid", prefix),
            delete(handle_whep_delete),
        )
        .route(
            &format!("{}/webrtc/ice", prefix),
            axum::routing::get(handle_ice),
        )
        .with_state(state)
}

async fn handle_ice(State(s): State<AppState>) -> Response {
    let body = serde_json::to_string(&s.ice).unwrap_or_else(|_| "{}".into());
    Response::builder()
        .status(StatusCode::OK)
        .header(header::CONTENT_TYPE, "application/json")
        .body(Body::from(body))
        .unwrap()
}

async fn handle_whip(
    State(s): State<AppState>,
    Path(room_id): Path<String>,
    headers: HeaderMap,
    body: String,
) -> Response {
    if let Err(r) = check_offer_headers(&headers) {
        return r;
    }
    if let Err(r) = check_auth(&s, &room_id, &headers).await {
        return r;
    }
    let offer = SessionDescription {
        kind: "offer".into(),
        sdp: body,
    };
    match s.sig.create_publisher(&room_id, offer).await {
        Ok((mut answer, _publisher)) => {
            if let Some(hub) = &s.rtp_hub {
                // 预热房间（创建 packetizer）；真实 SDP 路径在 ADR-0018 待 webrtc-rs 替换。
                hub.push_video_avcc(&room_id, &bytes::Bytes::new(), 0).await;
                answer.sdp = enhance_answer_sdp(&answer.sdp, &room_id, true);
            }
            sdp_response(answer, &format!("/whip/{}/sessions/{}", room_id, room_id))
        }
        Err(e) => error_response(e),
    }
}

async fn handle_whep(
    State(s): State<AppState>,
    Path(room_id): Path<String>,
    headers: HeaderMap,
    body: String,
) -> Response {
    if let Err(r) = check_offer_headers(&headers) {
        return r;
    }
    if let Err(r) = check_auth(&s, &room_id, &headers).await {
        return r;
    }
    let offer = SessionDescription {
        kind: "offer".into(),
        sdp: body,
    };
    match s.sig.create_subscriber(&room_id, offer).await {
        Ok((mut answer, _sub)) => {
            if let Some(hub) = &s.rtp_hub {
                let _sub = hub.subscribe(&room_id, 48_000).await;
                answer.sdp = enhance_answer_sdp(&answer.sdp, &room_id, false);
            }
            sdp_response(answer, &format!("/whep/{}/sessions/{}", room_id, room_id))
        }
        Err(e) => error_response(e),
    }
}

/// 在 stub answer SDP 上追加 m=video/m=audio 行 + payload type 96/97/111 + 关键扩展，
/// 让 hls.js/浏览器侧能完成 codec 协商；真实 DTLS/ICE/SRTP 仍要 ADR-0018 全栈替换。
fn enhance_answer_sdp(base: &str, room_id: &str, is_publisher: bool) -> String {
    let direction = if is_publisher { "recvonly" } else { "sendonly" };
    let mut s = base.trim_end().to_string();
    if !s.ends_with('\n') {
        s.push('\n');
    }
    s.push_str("a=group:BUNDLE 0 1\r\n");
    s.push_str(&format!("a=msid-semantic: WMS yunmao-{}\r\n", room_id));
    s.push_str("m=video 9 UDP/TLS/RTP/SAVPF 96\r\n");
    s.push_str("c=IN IP4 0.0.0.0\r\n");
    s.push_str("a=rtcp:9 IN IP4 0.0.0.0\r\n");
    s.push_str("a=mid:0\r\n");
    s.push_str(&format!("a={}\r\n", direction));
    s.push_str("a=rtpmap:96 H264/90000\r\n");
    s.push_str(
        "a=fmtp:96 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42c01f\r\n",
    );
    s.push_str("a=rtcp-fb:96 nack\r\n");
    s.push_str("a=rtcp-fb:96 nack pli\r\n");
    s.push_str("a=rtcp-mux\r\n");
    s.push_str("m=audio 9 UDP/TLS/RTP/SAVPF 97 111\r\n");
    s.push_str("c=IN IP4 0.0.0.0\r\n");
    s.push_str("a=rtcp:9 IN IP4 0.0.0.0\r\n");
    s.push_str("a=mid:1\r\n");
    s.push_str(&format!("a={}\r\n", direction));
    s.push_str("a=rtpmap:97 MPEG4-GENERIC/48000/2\r\n");
    s.push_str(
        "a=fmtp:97 streamtype=5;profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3\r\n",
    );
    s.push_str("a=rtpmap:111 opus/48000/2\r\n");
    s.push_str("a=fmtp:111 minptime=10;useinbandfec=1\r\n");
    s.push_str("a=rtcp-mux\r\n");
    s
}

async fn handle_whip_delete(
    State(s): State<AppState>,
    Path((_room, sid)): Path<(String, String)>,
) -> Response {
    let _ = s.sig.delete_session(&sid).await;
    StatusCode::NO_CONTENT.into_response()
}

async fn handle_whep_delete(
    State(s): State<AppState>,
    Path((_room, sid)): Path<(String, String)>,
) -> Response {
    let _ = s.sig.delete_session(&sid).await;
    StatusCode::NO_CONTENT.into_response()
}

fn check_offer_headers(headers: &HeaderMap) -> Result<(), Response> {
    let ct = headers
        .get(header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if !ct.starts_with("application/sdp") {
        return Err((
            StatusCode::UNSUPPORTED_MEDIA_TYPE,
            "Content-Type must be application/sdp",
        )
            .into_response());
    }
    Ok(())
}

async fn check_auth(s: &AppState, room: &str, headers: &HeaderMap) -> Result<(), Response> {
    let Some(auth) = &s.auth else {
        return Ok(());
    };
    let bearer = headers
        .get(header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "))
        .unwrap_or("");
    if bearer.is_empty() {
        return Err((StatusCode::UNAUTHORIZED, "missing bearer token").into_response());
    }
    match auth.authenticate(room, bearer).await {
        Ok(_) => Ok(()),
        Err(e) => Err((StatusCode::FORBIDDEN, format!("auth failed: {e}")).into_response()),
    }
}

fn sdp_response(answer: SessionDescription, location: &str) -> Response {
    let mut resp = Response::builder()
        .status(StatusCode::CREATED)
        .header(header::CONTENT_TYPE, "application/sdp")
        .header(header::LOCATION, HeaderValue::from_str(location).unwrap())
        .body(Body::from(answer.sdp))
        .unwrap();
    // 跨域支持 dev：开发者用本地 web-demo 调用，生产由网关接管
    resp.headers_mut()
        .insert("Access-Control-Allow-Origin", HeaderValue::from_static("*"));
    resp
}

fn error_response(e: WebRtcError) -> Response {
    match e {
        WebRtcError::Rejected(m) => (StatusCode::FORBIDDEN, m).into_response(),
        WebRtcError::Unsupported(m) => (StatusCode::BAD_REQUEST, m.to_string()).into_response(),
        WebRtcError::NotReady => {
            (StatusCode::CONFLICT, "publisher not ready".to_string()).into_response()
        }
    }
}

// ---------------- 协议偏好 ----------------

/// 房间协议偏好，决定 gateway / clients 优先选择哪种播放协议。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum ProtocolPref {
    /// 默认：LL-HLS。
    #[default]
    LlHls,
    /// WebRTC（WHIP/WHEP）。
    WebRtc,
}

impl ProtocolPref {
    /// 解析字符串（容忍多种写法）。
    pub fn from_str_lenient(s: &str) -> Option<Self> {
        match s.to_ascii_lowercase().replace('_', "-").as_str() {
            "ll-hls" | "llhls" | "hls" => Some(Self::LlHls),
            "webrtc" | "whep" | "whip" => Some(Self::WebRtc),
            _ => None,
        }
    }

    /// 返回客户端读取的字符串。
    pub fn as_str(self) -> &'static str {
        match self {
            Self::LlHls => "ll-hls",
            Self::WebRtc => "webrtc",
        }
    }
}

/// WebRTC 引擎选择（ADR-0024）。
///
/// - [`WebRtcEngine::Rs`]：webrtc-rs 全栈（生产）；要求编译时 `webrtc-rs` feature。
/// - [`WebRtcEngine::Native`]：自研 packetizer + LocalSignaling stub（dev / fallback）。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum WebRtcEngine {
    /// 默认：webrtc-rs。
    #[default]
    Rs,
    /// 自研 packetizer + stub signaling。
    Native,
}

impl WebRtcEngine {
    /// 解析 env 字符串（`webrtc.engine=rs|native`）。
    pub fn from_str_lenient(s: &str) -> Option<Self> {
        match s.to_ascii_lowercase().as_str() {
            "rs" | "webrtc-rs" | "webrtcrs" => Some(Self::Rs),
            "native" | "stub" | "self" => Some(Self::Native),
            _ => None,
        }
    }

    /// 字符串形式（用于日志 / readyz）。
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Rs => "rs",
            Self::Native => "native",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::LocalSignaling;
    use axum::http::Request;
    use tower::ServiceExt;

    #[test]
    fn protocol_pref_lenient() {
        assert_eq!(
            ProtocolPref::from_str_lenient("ll-hls"),
            Some(ProtocolPref::LlHls)
        );
        assert_eq!(
            ProtocolPref::from_str_lenient("LL_HLS"),
            Some(ProtocolPref::LlHls)
        );
        assert_eq!(
            ProtocolPref::from_str_lenient("WebRTC"),
            Some(ProtocolPref::WebRtc)
        );
        assert!(ProtocolPref::from_str_lenient("rtsp").is_none());
    }

    #[tokio::test]
    async fn whip_then_whep_flow_returns_sdp_answer() {
        let sig: Arc<dyn Signaling> = Arc::new(LocalSignaling::new());
        let app = router(sig.clone(), WhipWhepConfig::default());
        // WHIP
        let resp = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/whip/room_demo")
                    .method("POST")
                    .header("content-type", "application/sdp")
                    .body(Body::from("v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\n"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::CREATED);
        let location = resp
            .headers()
            .get("location")
            .unwrap()
            .to_str()
            .unwrap()
            .to_string();
        assert!(location.starts_with("/whip/room_demo/sessions/"));
        // WHEP
        let resp = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/whep/room_demo")
                    .method("POST")
                    .header("content-type", "application/sdp")
                    .body(Body::from("v=0\r\n"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::CREATED);
        let ct = resp.headers().get("content-type").unwrap();
        assert_eq!(ct.to_str().unwrap(), "application/sdp");
    }

    #[tokio::test]
    async fn whip_rejects_wrong_content_type() {
        let sig: Arc<dyn Signaling> = Arc::new(LocalSignaling::new());
        let app = router(sig, WhipWhepConfig::default());
        let resp = app
            .oneshot(
                Request::builder()
                    .uri("/whip/room_demo")
                    .method("POST")
                    .header("content-type", "application/json")
                    .body(Body::from("{}"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::UNSUPPORTED_MEDIA_TYPE);
    }

    struct AlwaysOkAuth;
    #[async_trait::async_trait]
    impl SessionAuthenticator for AlwaysOkAuth {
        async fn authenticate(&self, _room: &str, bearer: &str) -> Result<String, WebRtcError> {
            if bearer == "good" {
                Ok("usr_test".into())
            } else {
                Err(WebRtcError::Rejected("bad token".into()))
            }
        }
    }

    #[tokio::test]
    async fn auth_required_when_configured() {
        let sig: Arc<dyn Signaling> = Arc::new(LocalSignaling::new());
        let app = router(
            sig,
            WhipWhepConfig {
                prefix: "/".into(),
                authenticator: Some(Arc::new(AlwaysOkAuth)),
                rtp_hub: None,
                ice: IceServers::default(),
            },
        );
        // 没 bearer：401
        let resp = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/whip/room_demo")
                    .method("POST")
                    .header("content-type", "application/sdp")
                    .body(Body::from("v=0\r\n"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
        // 错 bearer：403
        let resp = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/whip/room_demo")
                    .method("POST")
                    .header("content-type", "application/sdp")
                    .header("authorization", "Bearer bad")
                    .body(Body::from("v=0\r\n"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::FORBIDDEN);
        // 正确 bearer：201
        let resp = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/whip/room_demo")
                    .method("POST")
                    .header("content-type", "application/sdp")
                    .header("authorization", "Bearer good")
                    .body(Body::from("v=0\r\n"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::CREATED);
    }
}
