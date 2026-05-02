use std::sync::Arc;
use std::time::Duration;

use axum::extract::ws::{Message, WebSocket};
use axum::extract::{Path, Query, State, WebSocketUpgrade};
use axum::http::StatusCode;
use axum::response::{IntoResponse, Json, Response};
use axum::routing::{get, post};
use axum::Router;
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use tokio::sync::mpsc;
use tracing::{info, warn};

use crate::state::AppState;
use crate::types::*;

type AppStateArc = Arc<AppState>;

// ── Router ──

pub fn router(state: AppState) -> Router {
    let state = Arc::new(state);
    Router::new()
        .route("/register", post(handle_register))
        .route("/pair-status/{token}", get(handle_pair_status))
        .route("/webhook", post(handle_webhook))
        .route("/ws/{token}", get(handle_ws))
        .route("/admin/killswitch", post(handle_admin_killswitch))
        .route("/admin/stats", get(handle_admin_stats))
        .route("/health", get(handle_health))
        .with_state(state)
}

// ── Query params ──

#[derive(Deserialize)]
struct SecretQuery {
    secret: Option<String>,
}

// ── Health ──

async fn handle_health() -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "healthy",
        version: env!("CARGO_PKG_VERSION"),
        commit: env!("GIT_HASH"),
    })
}

#[derive(Debug, Serialize)]
struct HealthResponse {
    status: &'static str,
    version: &'static str,
    commit: &'static str,
}

// ── Register ──

async fn handle_register(State(state): State<AppStateArc>) -> Response {
    let token = uuid::Uuid::new_v4().to_string().replace('-', "");
    let pair_code = format!("{:06}", rand::random::<u32>() % 900_000 + 100_000);

    if let Err(e) = state.store.put_token(&token).await {
        warn!("put_token error: {e}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    state
        .pair_codes
        .insert(pair_code.clone(), token.clone())
        .await;

    Json(RegisterResponse {
        token,
        pair_code,
        channel_url: state.config.channel_url.clone(),
    })
    .into_response()
}

// ── Pair Status ──

async fn handle_pair_status(
    State(state): State<AppStateArc>,
    Path(token): Path<String>,
) -> Json<PairStatusResponse> {
    let paired = state.paired_markers.get(&token).await.unwrap_or(false);
    Json(PairStatusResponse { paired })
}

// ── Webhook ──

async fn handle_webhook(
    State(state): State<AppStateArc>,
    Query(q): Query<SecretQuery>,
    axum::Json(payload): axum::Json<KakaoPayload>,
) -> Response {
    // Auth
    if !check_secret(&state, q.secret.as_deref()) {
        return StatusCode::UNAUTHORIZED.into_response();
    }

    let action_id = &payload.action.id;
    let utterance = &payload.user_request.utterance;
    let user_id = &payload.user_request.user.id;
    let callback_url = payload.user_request.callback_url.as_deref();

    if action_id.is_empty() || utterance.is_empty() || user_id.is_empty() {
        return (StatusCode::BAD_REQUEST, "missing required fields").into_response();
    }

    // Killswitch
    if state.store.get_killswitch().await.unwrap_or(false) {
        return (StatusCode::SERVICE_UNAVAILABLE, "Service temporarily suspended").into_response();
    }

    // Rate limit
    let cap = state
        .store
        .check_rate_limit(state.config.daily_limit, state.config.monthly_limit)
        .await;
    match cap {
        Ok(r) if !r.ok => return Json(kakao_text(MSG_RATE_LIMITED)).into_response(),
        Err(e) => {
            warn!("rate limit check error: {e}");
            return Json(kakao_text(MSG_TRANSIENT_ERROR)).into_response();
        }
        _ => {}
    }

    // Pairing: 6-digit utterance
    if utterance.len() == 6 && utterance.chars().all(|c| c.is_ascii_digit()) {
        return handle_pairing(&state, utterance, user_id).await;
    }

    // No callbackUrl = OpenBuilder test mode
    let callback_url = match callback_url {
        Some(url) if !url.is_empty() => url.to_string(),
        _ => return Json(kakao_text(MSG_NO_CALLBACK)).into_response(),
    };

    // Lookup user mapping
    let relay_token = match state.store.get_user_mapping(user_id).await {
        Ok(Some(t)) => t,
        Ok(None) => return Json(kakao_text(MSG_NOT_PAIRED)).into_response(),
        Err(e) => {
            warn!("user mapping lookup error: {e}");
            return Json(kakao_text(MSG_TRANSIENT_ERROR)).into_response();
        }
    };

    // Check if WS session is online
    let sender = match state.sessions.get(&relay_token) {
        Some(s) => s.clone(),
        None => return Json(kakao_text(MSG_OFFLINE)).into_response(),
    };

    // SSRF guard
    if !is_allowed_callback_host(&callback_url) {
        warn!("SSRF blocked: {callback_url}");
        return Json(kakao_text(MSG_TRANSIENT_ERROR)).into_response();
    }

    // Store pending callback
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64;
    let pending = PendingContext {
        callback_url,
        user_id: user_id.to_string(),
        created_at: now,
    };
    if let Err(e) = state.store.put_pending(action_id, &pending).await {
        warn!("put pending error: {e}");
        return Json(kakao_text(MSG_TRANSIENT_ERROR)).into_response();
    }

    // Send to WS
    let ws_frame = WsOutgoing {
        id: action_id.to_string(),
        text: utterance.to_string(),
        user_id: user_id.to_string(),
    };
    let json = serde_json::to_string(&ws_frame).unwrap();
    if sender.send(Message::Text(json.into())).is_err() {
        warn!("WS send failed for token {relay_token}");
        return Json(kakao_text(MSG_OFFLINE)).into_response();
    }

    Json(kakao_async_ack()).into_response()
}

async fn handle_pairing(state: &AppStateArc, pair_code: &str, kakao_user_id: &str) -> Response {
    let token = match state.pair_codes.get(pair_code).await {
        Some(t) => t,
        None => return Json(kakao_text(MSG_INVALID_PAIR_CODE)).into_response(),
    };

    // Consume the pair code
    state.pair_codes.invalidate(pair_code).await;

    // Create user mapping
    if let Err(e) = state.store.put_user_mapping(kakao_user_id, &token).await {
        warn!("put user mapping error: {e}");
        return Json(kakao_text(MSG_TRANSIENT_ERROR)).into_response();
    }

    // Set paired marker for GUI polling
    state.paired_markers.insert(token, true).await;

    Json(kakao_text(MSG_PAIRED)).into_response()
}

// ── WebSocket ──

async fn handle_ws(
    State(state): State<AppStateArc>,
    Path(token): Path<String>,
    ws: WebSocketUpgrade,
) -> Response {
    // Validate token
    match state.store.token_exists(&token).await {
        Ok(true) => {}
        Ok(false) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(e) => {
            warn!("token check error: {e}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }

    ws.on_upgrade(move |socket| ws_session(state, token, socket))
}

async fn ws_session(state: AppStateArc, token: String, socket: WebSocket) {
    let (mut ws_sink, mut ws_stream) = socket.split();
    let (tx, mut rx) = mpsc::unbounded_channel::<Message>();

    // Register session
    state.sessions.insert(token.clone(), tx);
    info!("WS connected: {token} (sessions: {})", state.sessions.len());

    // Writer task: forward mpsc messages to WS sink + periodic ping
    let writer = tokio::spawn(async move {
        let mut ping_interval = tokio::time::interval(Duration::from_secs(30));
        loop {
            tokio::select! {
                msg = rx.recv() => {
                    match msg {
                        Some(m) => {
                            if ws_sink.send(m).await.is_err() {
                                break;
                            }
                        }
                        None => break,
                    }
                }
                _ = ping_interval.tick() => {
                    if ws_sink.send(Message::Ping(vec![].into())).await.is_err() {
                        break;
                    }
                }
            }
        }
        let _ = ws_sink.close().await;
    });

    // Reader task: process incoming WS frames
    let reader_state = state.clone();
    let reader_token = token.clone();
    let reader = tokio::spawn(async move {
        let mut pong_timeout = tokio::time::interval(Duration::from_secs(60));
        pong_timeout.tick().await; // skip first immediate tick

        loop {
            tokio::select! {
                frame = ws_stream.next() => {
                    match frame {
                        Some(Ok(Message::Text(text))) => {
                            handle_ws_message(&reader_state, &text).await;
                        }
                        Some(Ok(Message::Pong(_))) => {
                            pong_timeout.reset();
                        }
                        Some(Ok(Message::Close(_))) | None => break,
                        Some(Err(e)) => {
                            warn!("WS read error for {reader_token}: {e}");
                            break;
                        }
                        _ => {} // Binary, Ping handled by axum
                    }
                }
                _ = pong_timeout.tick() => {
                    warn!("WS pong timeout for {reader_token}");
                    break;
                }
            }
        }
    });

    // Wait for either task to finish, then clean up
    tokio::select! {
        _ = writer => {}
        _ = reader => {}
    }

    state.sessions.remove(&token);
    info!("WS disconnected: {token} (sessions: {})", state.sessions.len());
}

async fn handle_ws_message(state: &AppStateArc, text: &str) {
    let incoming: WsIncoming = match serde_json::from_str(text) {
        Ok(m) => m,
        Err(e) => {
            warn!("malformed WS frame: {e}");
            return;
        }
    };

    if incoming.id.is_empty() || incoming.text.is_empty() {
        warn!("WS frame missing id/text");
        return;
    }

    dispatch_callback(state, incoming).await;
}

async fn dispatch_callback(state: &AppStateArc, incoming: WsIncoming) {
    // Atomic claim
    let pending = match state.store.take_pending(&incoming.id).await {
        Ok(Some(p)) => p,
        Ok(None) => {
            warn!("no pending entry for action {}", incoming.id);
            return;
        }
        Err(e) => {
            warn!("take pending error: {e}");
            return;
        }
    };

    // SSRF guard
    if !is_allowed_callback_host(&pending.callback_url) {
        warn!("SSRF blocked on dispatch: {}", pending.callback_url);
        return;
    }

    // Fire-and-forget callback to Kakao
    let body = match incoming.image_url.as_deref() {
        Some(url) if is_public_https_image_url(url) => {
            kakao_image(url, incoming.image_alt.as_deref().unwrap_or("image"))
        }
        _ => kakao_text(&incoming.text),
    };
    if let Err(e) = state
        .http_client
        .post(&pending.callback_url)
        .json(&body)
        .send()
        .await
    {
        warn!("callback dispatch failed: {e}");
    }
}

// ── Admin ──

async fn handle_admin_killswitch(
    State(state): State<AppStateArc>,
    Query(q): Query<SecretQuery>,
    axum::Json(body): axum::Json<KillswitchRequest>,
) -> Response {
    if !check_secret(&state, q.secret.as_deref()) {
        return StatusCode::UNAUTHORIZED.into_response();
    }

    if let Err(e) = state.store.set_killswitch(body.enabled).await {
        warn!("set killswitch error: {e}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    Json(KillswitchResponse {
        killswitch: body.enabled,
    })
    .into_response()
}

#[derive(Deserialize)]
struct KillswitchRequest {
    enabled: bool,
}

async fn handle_admin_stats(
    State(state): State<AppStateArc>,
    Query(q): Query<SecretQuery>,
) -> Response {
    if !check_secret(&state, q.secret.as_deref()) {
        return StatusCode::UNAUTHORIZED.into_response();
    }

    let stats = match state.store.get_stats().await {
        Ok(s) => s,
        Err(e) => {
            warn!("get stats error: {e}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    let killswitch = state.store.get_killswitch().await.unwrap_or(false);

    Json(AdminStatsResponse {
        daily: LimitInfo {
            current: stats.daily,
            limit: state.config.daily_limit,
        },
        monthly: LimitInfo {
            current: stats.monthly,
            limit: state.config.monthly_limit,
        },
        killswitch,
        ws_sessions: state.sessions.len(),
        rss_bytes: get_rss_bytes(),
        fd_count: get_fd_count(),
    })
    .into_response()
}

// ── Process metrics ──

fn get_rss_bytes() -> u64 {
    #[cfg(target_os = "macos")]
    {
        #[repr(C)]
        struct MachTaskBasicInfo {
            virtual_size: u64,
            resident_size: u64,
            resident_size_max: u64,
            user_time: [u32; 2],
            system_time: [u32; 2],
            policy: i32,
            suspend_count: i32,
        }

        extern "C" {
            fn mach_task_self() -> u32;
            fn task_info(
                task: u32,
                flavor: u32,
                info: *mut MachTaskBasicInfo,
                count: *mut u32,
            ) -> i32;
        }

        const MACH_TASK_BASIC_INFO: u32 = 20;

        unsafe {
            let mut info: MachTaskBasicInfo = std::mem::zeroed();
            let mut count =
                (std::mem::size_of::<MachTaskBasicInfo>() / std::mem::size_of::<u32>()) as u32;
            if task_info(mach_task_self(), MACH_TASK_BASIC_INFO, &mut info, &mut count) == 0 {
                return info.resident_size;
            }
        }
        0
    }
    #[cfg(target_os = "linux")]
    {
        // /proc/self/statm: field[1] = RSS in pages
        std::fs::read_to_string("/proc/self/statm")
            .ok()
            .and_then(|s| s.split_whitespace().nth(1)?.parse::<u64>().ok())
            .map(|pages| pages * 4096)
            .unwrap_or(0)
    }
    #[cfg(not(any(target_os = "macos", target_os = "linux")))]
    {
        0
    }
}

fn get_fd_count() -> u64 {
    #[cfg(target_os = "macos")]
    let path = "/dev/fd";
    #[cfg(not(target_os = "macos"))]
    let path = "/proc/self/fd";

    std::fs::read_dir(path)
        .map(|entries| entries.count() as u64)
        .unwrap_or(0)
}

// ── Helpers ──

fn check_secret(state: &AppStateArc, secret: Option<&str>) -> bool {
    let expected = &state.config.webhook_secret;
    if expected.is_empty() {
        return false;
    }
    match secret {
        Some(s) => s == expected,
        None => false,
    }
}

pub fn is_allowed_callback_host(url: &str) -> bool {
    let parsed = match url::Url::parse(url) {
        Ok(u) => u,
        Err(_) => return false,
    };
    if parsed.scheme() != "https" {
        return false;
    }
    let host = match parsed.host_str() {
        Some(h) => h.to_string(),
        None => return false,
    };
    (host == "kakao.com" || host.ends_with(".kakao.com"))
        || (host == "kakaoenterprise.com" || host.ends_with(".kakaoenterprise.com"))
}

fn is_public_https_image_url(raw: &str) -> bool {
    let parsed = match url::Url::parse(raw) {
        Ok(u) => u,
        Err(_) => return false,
    };
    parsed.scheme() == "https" && parsed.host_str().is_some()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ssrf_guard_allows_kakao() {
        assert!(is_allowed_callback_host("https://callback.kakao.com/abc"));
        assert!(is_allowed_callback_host(
            "https://api.kakaoenterprise.com/v1"
        ));
    }

    #[test]
    fn ssrf_guard_blocks_others() {
        assert!(!is_allowed_callback_host("https://evil.com/steal"));
        assert!(!is_allowed_callback_host("https://kakao.com.evil.com/x"));
        assert!(!is_allowed_callback_host("not-a-url"));
        assert!(!is_allowed_callback_host(""));
    }

    #[test]
    fn ssrf_guard_blocks_subdomain_trick() {
        assert!(is_allowed_callback_host("https://kakao.com/path"));
        assert!(!is_allowed_callback_host("https://fakekakao.com/path"));
    }

    #[test]
    fn ssrf_guard_blocks_non_https() {
        assert!(!is_allowed_callback_host("http://callback.kakao.com/abc"));
        assert!(!is_allowed_callback_host(
            "file://kakao.com/etc/passwd"
        ));
        assert!(!is_allowed_callback_host(
            "javascript://kakao.com/xss"
        ));
    }
}
