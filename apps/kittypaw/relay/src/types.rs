use serde::{Deserialize, Serialize};
use std::env;

// ── Korean message constants (byte-identical to TS original) ──

pub const MSG_RATE_LIMITED: &str = "일일 사용 한도에 도달했습니다.";
pub const MSG_NO_CALLBACK: &str = "KittyPaw 스킬 서버가 정상 동작 중입니다. 오픈빌더에서 비동기 콜백을 활성화하면 AI 응답을 받을 수 있습니다.";
pub const MSG_NOT_PAIRED: &str = "KittyPaw와 연결이 필요합니다. KittyPaw 앱에서 연결 코드를 확인하세요.";
pub const MSG_TRANSIENT_ERROR: &str = "일시적인 오류가 발생했습니다. 잠시 후 다시 시도해주세요.";
pub const MSG_OFFLINE: &str = "KittyPaw가 현재 오프라인 상태입니다. 앱을 실행 후 다시 시도해 주세요.";
pub const MSG_INVALID_PAIR_CODE: &str = "유효하지 않은 연결 코드입니다. KittyPaw 앱에서 새 코드를 확인하세요.";
pub const MSG_PAIRED: &str = "연결 완료!";
pub const MSG_PROCESSING: &str = "처리 중입니다...";

// ── Kakao Inbound (webhook payload) ──

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct KakaoPayload {
    pub action: KakaoAction,
    pub user_request: KakaoUserRequest,
}

#[derive(Debug, Deserialize)]
pub struct KakaoAction {
    pub id: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct KakaoUserRequest {
    pub utterance: String,
    pub user: KakaoUser,
    pub callback_url: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct KakaoUser {
    pub id: String,
}

// ── Kakao Outbound (callback + sync responses) ──

#[derive(Debug, Serialize)]
pub struct KakaoSimpleResponse {
    pub version: &'static str,
    pub template: KakaoTemplate,
}

#[derive(Debug, Serialize)]
pub struct KakaoTemplate {
    pub outputs: Vec<KakaoOutput>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct KakaoOutput {
    pub simple_text: KakaoSimpleText,
}

#[derive(Debug, Serialize)]
pub struct KakaoSimpleText {
    pub text: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct KakaoAsyncAck {
    pub version: &'static str,
    pub use_callback: bool,
    pub data: KakaoAsyncData,
}

#[derive(Debug, Serialize)]
pub struct KakaoAsyncData {
    pub text: String,
}

/// Build a standard Kakao simple text response.
pub fn kakao_text(text: &str) -> KakaoSimpleResponse {
    KakaoSimpleResponse {
        version: "2.0",
        template: KakaoTemplate {
            outputs: vec![KakaoOutput {
                simple_text: KakaoSimpleText {
                    text: text.to_string(),
                },
            }],
        },
    }
}

/// Build the async callback acknowledgment response.
pub fn kakao_async_ack() -> KakaoAsyncAck {
    KakaoAsyncAck {
        version: "2.0",
        use_callback: true,
        data: KakaoAsyncData {
            text: MSG_PROCESSING.to_string(),
        },
    }
}

// ── WebSocket frame types (Go client contract) ──

/// Frame sent TO the Go client: {"id","text","user_id"} (snake_case)
#[derive(Debug, Serialize)]
pub struct WsOutgoing {
    pub id: String,
    pub text: String,
    pub user_id: String,
}

/// Frame received FROM the Go client: {"id","text"}
#[derive(Debug, Deserialize)]
pub struct WsIncoming {
    pub id: String,
    pub text: String,
}

// ── Pending callback context (SQLite persisted) ──

#[derive(Debug, Clone)]
pub struct PendingContext {
    pub callback_url: String,
    pub user_id: String,
    pub created_at: i64,
}

// ── API response types ──

#[derive(Debug, Serialize)]
pub struct RegisterResponse {
    pub token: String,
    pub pair_code: String,
    pub channel_url: String,
}

#[derive(Debug, Serialize)]
pub struct PairStatusResponse {
    pub paired: bool,
}

#[derive(Debug, Serialize)]
pub struct AdminStatsResponse {
    pub daily: LimitInfo,
    pub monthly: LimitInfo,
    pub killswitch: bool,
}

#[derive(Debug, Serialize)]
pub struct LimitInfo {
    pub current: u64,
    pub limit: u64,
}

#[derive(Debug, Serialize)]
pub struct KillswitchResponse {
    pub killswitch: bool,
}

// ── Store result types ──

#[derive(Debug)]
pub struct RateLimitResult {
    pub ok: bool,
    pub daily: u64,
    pub monthly: u64,
}

#[derive(Debug)]
pub struct Stats {
    pub daily: u64,
    pub monthly: u64,
}

// ── Config ──

#[derive(Debug, Clone)]
pub struct Config {
    pub webhook_secret: String,
    pub daily_limit: u64,
    pub monthly_limit: u64,
    pub channel_url: String,
    pub database_path: String,
    pub bind_addr: String,
}

impl Config {
    pub fn from_env() -> Self {
        Self {
            webhook_secret: env::var("WEBHOOK_SECRET").unwrap_or_default(),
            daily_limit: env::var("DAILY_LIMIT")
                .ok()
                .and_then(|v| v.parse().ok())
                .unwrap_or(10_000),
            monthly_limit: env::var("MONTHLY_LIMIT")
                .ok()
                .and_then(|v| v.parse().ok())
                .unwrap_or(100_000),
            channel_url: env::var("CHANNEL_URL")
                .unwrap_or_else(|_| "https://pf.kakao.com/_exjFdX/chat".to_string()),
            database_path: env::var("DATABASE_PATH")
                .unwrap_or_else(|_| "relay.db".to_string()),
            bind_addr: env::var("BIND_ADDR")
                .unwrap_or_else(|_| "0.0.0.0:8787".to_string()),
        }
    }
}

// ── Tests ──

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn kakao_payload_deserializes() {
        let json = r#"{
            "action": { "id": "act_123" },
            "userRequest": {
                "utterance": "hello",
                "user": { "id": "user_456" },
                "callbackUrl": "https://callback.kakao.com/abc"
            }
        }"#;
        let payload: KakaoPayload = serde_json::from_str(json).unwrap();
        assert_eq!(payload.action.id, "act_123");
        assert_eq!(payload.user_request.utterance, "hello");
        assert_eq!(payload.user_request.user.id, "user_456");
        assert_eq!(
            payload.user_request.callback_url.as_deref(),
            Some("https://callback.kakao.com/abc")
        );
    }

    #[test]
    fn kakao_payload_without_callback() {
        let json = r#"{
            "action": { "id": "act_123" },
            "userRequest": {
                "utterance": "hello",
                "user": { "id": "user_456" }
            }
        }"#;
        let payload: KakaoPayload = serde_json::from_str(json).unwrap();
        assert!(payload.user_request.callback_url.is_none());
    }

    #[test]
    fn ws_outgoing_serializes_snake_case() {
        let frame = WsOutgoing {
            id: "act_123".to_string(),
            text: "hello".to_string(),
            user_id: "user_456".to_string(),
        };
        let json = serde_json::to_string(&frame).unwrap();
        assert!(json.contains("\"user_id\""));
        assert!(!json.contains("\"userId\""));
    }

    #[test]
    fn ws_incoming_deserializes() {
        let json = r#"{"id":"act_123","text":"response text"}"#;
        let frame: WsIncoming = serde_json::from_str(json).unwrap();
        assert_eq!(frame.id, "act_123");
        assert_eq!(frame.text, "response text");
    }

    #[test]
    fn kakao_callback_body_serializes_correctly() {
        let resp = kakao_text("테스트 메시지");
        let json = serde_json::to_string(&resp).unwrap();
        assert!(json.contains("\"simpleText\""));
        assert!(json.contains("\"테스트 메시지\""));
        assert!(json.contains("\"version\":\"2.0\""));
    }

    #[test]
    fn kakao_async_ack_uses_camel_case() {
        let ack = kakao_async_ack();
        let json = serde_json::to_string(&ack).unwrap();
        assert!(json.contains("\"useCallback\":true"));
        assert!(!json.contains("\"use_callback\""));
        assert!(json.contains(MSG_PROCESSING));
    }

    #[test]
    fn config_defaults() {
        // Clear env vars to test defaults
        env::remove_var("WEBHOOK_SECRET");
        env::remove_var("DAILY_LIMIT");
        env::remove_var("MONTHLY_LIMIT");
        env::remove_var("CHANNEL_URL");
        env::remove_var("DATABASE_PATH");
        env::remove_var("BIND_ADDR");

        let config = Config::from_env();
        assert_eq!(config.daily_limit, 10_000);
        assert_eq!(config.monthly_limit, 100_000);
        assert_eq!(config.bind_addr, "0.0.0.0:8787");
        assert_eq!(config.database_path, "relay.db");
    }
}
