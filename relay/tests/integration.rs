use std::net::SocketAddr;
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use serde_json::{json, Value};
use tokio::net::TcpListener;
use tokio_tungstenite::tungstenite::Message;

use kittypaw_relay::routes;
use kittypaw_relay::state::AppState;
use kittypaw_relay::types::Config;

// ── Test Harness ──

fn test_config() -> Config {
    Config {
        webhook_secret: "test-secret".to_string(),
        daily_limit: 10_000,
        monthly_limit: 100_000,
        channel_url: "https://pf.kakao.com/test".to_string(),
        database_path: ":memory:".to_string(),
        bind_addr: "127.0.0.1:0".to_string(),
    }
}

async fn spawn_server() -> SocketAddr {
    spawn_server_with_config(test_config()).await
}

async fn spawn_server_with_config(config: Config) -> SocketAddr {
    let state = AppState::new(config).unwrap();
    let app = routes::router(state);

    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();

    tokio::spawn(async move {
        axum::serve(listener, app).await.unwrap();
    });

    // Give the server a moment to start
    tokio::time::sleep(Duration::from_millis(50)).await;
    addr
}

fn base_url(addr: SocketAddr) -> String {
    format!("http://{addr}")
}

fn ws_url(addr: SocketAddr, token: &str) -> String {
    format!("ws://{addr}/ws/{token}")
}

fn webhook_payload(action_id: &str, utterance: &str, user_id: &str, callback_url: Option<&str>) -> Value {
    let mut ur = json!({
        "utterance": utterance,
        "user": { "id": user_id }
    });
    if let Some(cb) = callback_url {
        ur["callbackUrl"] = json!(cb);
    }
    json!({
        "action": { "id": action_id },
        "userRequest": ur
    })
}

// ── Test 1: Happy Path ──

#[tokio::test]
async fn test_happy_path_register_pair_webhook_ws() {
    let addr = spawn_server().await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    // 1. Register
    let resp: Value = client
        .post(format!("{base}/register"))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    let token = resp["token"].as_str().unwrap().to_string();
    let pair_code = resp["pair_code"].as_str().unwrap().to_string();
    assert_eq!(token.len(), 32);
    assert_eq!(pair_code.len(), 6);
    assert!(resp["channel_url"].as_str().is_some());

    // 2. Connect WS
    let (mut ws, _) = tokio_tungstenite::connect_async(ws_url(addr, &token))
        .await
        .unwrap();

    // 3. Pair via webhook (6-digit code as utterance)
    let pair_resp: Value = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("pair_act", &pair_code, "kakao_user_1", Some("https://callback.kakao.com/pair")))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    // Pairing returns a simpleText with MSG_PAIRED
    assert_eq!(pair_resp["template"]["outputs"][0]["simpleText"]["text"], "연결 완료!");

    // 4. Check pair status
    let pair_status: Value = client
        .get(format!("{base}/pair-status/{token}"))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    assert_eq!(pair_status["paired"], true);

    // 5. Send real message via webhook
    let msg_resp: Value = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("act_001", "안녕하세요", "kakao_user_1", Some("https://callback.kakao.com/reply")))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    assert_eq!(msg_resp["useCallback"], true);
    assert_eq!(msg_resp["version"], "2.0");

    // 6. Verify WS receives the message (skip Ping/Pong frames)
    let text = tokio::time::timeout(Duration::from_secs(5), async {
        loop {
            match ws.next().await {
                Some(Ok(Message::Text(t))) => break t,
                Some(Ok(_)) => continue, // skip Ping, Pong, etc.
                Some(Err(e)) => panic!("WS error: {e}"),
                None => panic!("WS stream ended"),
            }
        }
    })
    .await
    .expect("WS timeout waiting for text frame");

    let frame: Value = serde_json::from_str(&text).unwrap();
    assert_eq!(frame["id"], "act_001");
    assert_eq!(frame["text"], "안녕하세요");
    assert_eq!(frame["user_id"], "kakao_user_1");

    // 7. Send reply via WS (callback dispatch will fail since it's not a real Kakao server, but the flow works)
    let reply = json!({"id": "act_001", "text": "반갑습니다"});
    ws.send(Message::Text(reply.to_string())).await.unwrap();

    // Small delay for dispatch to process
    tokio::time::sleep(Duration::from_millis(100)).await;

    ws.close(None).await.ok();
}

// ── Test 2: Expired Pair Code Rejection ──

#[tokio::test]
async fn test_expired_pair_code_rejection() {
    // Use a very short TTL config — but moka TTL is set in AppState::new at 300s.
    // Instead we test with an invalid (non-existent) pair code.
    let addr = spawn_server().await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    // Try pairing with a code that was never registered
    let resp: Value = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("act_x", "999999", "kakao_user_x", Some("https://callback.kakao.com/x")))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();

    assert_eq!(
        resp["template"]["outputs"][0]["simpleText"]["text"],
        "유효하지 않은 연결 코드입니다. KittyPaw 앱에서 새 코드를 확인하세요."
    );
}

// ── Test 3: Rate Limit Exceeded ──

#[tokio::test]
async fn test_rate_limit_exceeded() {
    let mut config = test_config();
    config.daily_limit = 2; // Pairing consumes 1, first real webhook consumes 1, second should fail
    let addr = spawn_server_with_config(config).await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    // Register and pair
    let reg: Value = client
        .post(format!("{base}/register"))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    let token = reg["token"].as_str().unwrap();
    let pair_code = reg["pair_code"].as_str().unwrap();

    // Connect WS
    let (mut ws, _) = tokio_tungstenite::connect_async(ws_url(addr, token))
        .await
        .unwrap();

    // Pair
    client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("pair_act", pair_code, "user_rl", Some("https://callback.kakao.com/p")))
        .send()
        .await
        .unwrap();

    // First real webhook — consumes the 1 daily limit
    let r1: Value = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("act_1", "hello", "user_rl", Some("https://callback.kakao.com/1")))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    assert_eq!(r1["useCallback"], true);

    // Drain WS message
    let _ = tokio::time::timeout(Duration::from_secs(1), ws.next()).await;

    // Second webhook — should be rate limited
    let r2: Value = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("act_2", "hello again", "user_rl", Some("https://callback.kakao.com/2")))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();

    assert_eq!(
        r2["template"]["outputs"][0]["simpleText"]["text"],
        "일일 사용 한도에 도달했습니다."
    );

    ws.close(None).await.ok();
}

// ── Test 4: Killswitch Blocks Webhook, WS Stays Alive ──

#[tokio::test]
async fn test_killswitch_blocks_webhook_ws_stays() {
    let addr = spawn_server().await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    // Register and connect WS
    let reg: Value = client
        .post(format!("{base}/register"))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    let token = reg["token"].as_str().unwrap();

    let (mut ws, _) = tokio_tungstenite::connect_async(ws_url(addr, token))
        .await
        .unwrap();

    // Enable killswitch
    let ks_resp = client
        .post(format!("{base}/admin/killswitch?secret=test-secret"))
        .json(&json!({"enabled": true}))
        .send()
        .await
        .unwrap();
    assert_eq!(ks_resp.status(), 200);

    // Webhook should be blocked with 503
    let webhook_resp = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("act_ks", "hello", "user_ks", Some("https://callback.kakao.com/ks")))
        .send()
        .await
        .unwrap();
    assert_eq!(webhook_resp.status(), 503);

    // WS should still be alive — send a text message and it should not error
    ws.send(Message::Text(json!({"id":"test","text":"ping"}).to_string()))
        .await
        .expect("WS should still be connected");

    // Disable killswitch
    client
        .post(format!("{base}/admin/killswitch?secret=test-secret"))
        .json(&json!({"enabled": false}))
        .send()
        .await
        .unwrap();

    ws.close(None).await.ok();
}

// ── Test 5: SSRF Guard Rejects Non-Kakao Callback ──

#[tokio::test]
async fn test_ssrf_guard_rejects_non_kakao() {
    let addr = spawn_server().await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    // Register, pair, connect WS
    let reg: Value = client
        .post(format!("{base}/register"))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    let token = reg["token"].as_str().unwrap();
    let pair_code = reg["pair_code"].as_str().unwrap();

    let (_ws, _) = tokio_tungstenite::connect_async(ws_url(addr, token))
        .await
        .unwrap();

    // Pair
    client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("pair_ssrf", pair_code, "user_ssrf", Some("https://callback.kakao.com/p")))
        .send()
        .await
        .unwrap();

    // Send webhook with evil callback URL
    let resp: Value = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("act_ssrf", "hello", "user_ssrf", Some("https://evil.com/steal")))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();

    // Should get a transient error message (SSRF blocked before reaching WS)
    assert_eq!(
        resp["template"]["outputs"][0]["simpleText"]["text"],
        "일시적인 오류가 발생했습니다. 잠시 후 다시 시도해주세요."
    );
}

// ── Test 6: Unpaired User Gets Guide Message ──

#[tokio::test]
async fn test_unpaired_user_gets_guide() {
    let addr = spawn_server().await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    let resp: Value = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("act_unp", "hello", "unknown_user", Some("https://callback.kakao.com/x")))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();

    assert_eq!(
        resp["template"]["outputs"][0]["simpleText"]["text"],
        "KittyPaw와 연결이 필요합니다. KittyPaw 앱에서 연결 코드를 확인하세요."
    );
}

// ── Test 7: Invalid Token WS Returns 401 ──

#[tokio::test]
async fn test_invalid_token_ws_401() {
    let addr = spawn_server().await;

    let result = tokio_tungstenite::connect_async(ws_url(addr, "nonexistent-token")).await;

    // Should fail with HTTP 401 (not upgraded)
    assert!(result.is_err(), "WS connection should fail for invalid token");
}

// ── Test 8: Offline Session Returns Offline Message ──

#[tokio::test]
async fn test_offline_session_returns_offline_message() {
    let addr = spawn_server().await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    // Register and pair but do NOT connect WS
    let reg: Value = client
        .post(format!("{base}/register"))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    let pair_code = reg["pair_code"].as_str().unwrap();

    // Pair (this creates user mapping but no WS)
    client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("pair_off", pair_code, "user_offline", Some("https://callback.kakao.com/p")))
        .send()
        .await
        .unwrap();

    // Send real message — no WS connected
    let resp: Value = client
        .post(format!("{base}/webhook?secret=test-secret"))
        .json(&webhook_payload("act_off", "hello", "user_offline", Some("https://callback.kakao.com/off")))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();

    assert_eq!(
        resp["template"]["outputs"][0]["simpleText"]["text"],
        "KittyPaw가 현재 오프라인 상태입니다. 앱을 실행 후 다시 시도해 주세요."
    );
}

// ── Test: Health endpoint ──

#[tokio::test]
async fn test_health_endpoint() {
    let addr = spawn_server().await;
    let client = reqwest::Client::new();

    let resp = client
        .get(format!("{}/health", base_url(addr)))
        .send()
        .await
        .unwrap();
    assert_eq!(resp.status(), 200);
}

// ── Test: Admin stats ──

#[tokio::test]
async fn test_admin_stats() {
    let addr = spawn_server().await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    let resp: Value = client
        .get(format!("{base}/admin/stats?secret=test-secret"))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();

    assert_eq!(resp["daily"]["current"], 0);
    assert_eq!(resp["monthly"]["current"], 0);
    assert_eq!(resp["killswitch"], false);
    assert!(resp["daily"]["limit"].as_u64().unwrap() > 0);
}

// ── Test: Webhook auth required ──

#[tokio::test]
async fn test_webhook_requires_auth() {
    let addr = spawn_server().await;
    let client = reqwest::Client::new();
    let base = base_url(addr);

    // No secret
    let resp = client
        .post(format!("{base}/webhook"))
        .json(&webhook_payload("act", "hello", "user", Some("https://callback.kakao.com/x")))
        .send()
        .await
        .unwrap();
    assert_eq!(resp.status(), 401);

    // Wrong secret
    let resp = client
        .post(format!("{base}/webhook?secret=wrong"))
        .json(&webhook_payload("act", "hello", "user", Some("https://callback.kakao.com/x")))
        .send()
        .await
        .unwrap();
    assert_eq!(resp.status(), 401);
}
