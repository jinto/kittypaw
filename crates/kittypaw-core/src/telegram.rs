use crate::error::{KittypawError, Result};

/// Fetch the most recent chat_id from Telegram Bot API getUpdates.
///
/// The user must send at least one message to the bot before calling this.
pub async fn fetch_chat_id(token: &str) -> Result<String> {
    let url = format!("https://api.telegram.org/bot{token}/getUpdates?limit=1&offset=-1");
    let client = reqwest::Client::new();
    let resp = client
        .get(&url)
        .timeout(std::time::Duration::from_secs(10))
        .send()
        .await
        .map_err(|e| KittypawError::Skill(format!("Telegram API 요청 실패: {e}")))?;

    let body: serde_json::Value = resp
        .json()
        .await
        .map_err(|e| KittypawError::Skill(format!("Telegram 응답 파싱 실패: {e}")))?;

    if body["ok"].as_bool() != Some(true) {
        return Err(KittypawError::Config("봇 토큰이 유효하지 않습니다".into()));
    }

    let results = body["result"].as_array().ok_or_else(|| {
        KittypawError::Skill("결과가 비어있습니다. 봇에게 먼저 메시지를 보내주세요".into())
    })?;

    if results.is_empty() {
        return Err(KittypawError::Skill(
            "봇에게 먼저 메시지를 보내주세요".into(),
        ));
    }

    for result in results {
        if let Some(id) = result["message"]["chat"]["id"].as_i64() {
            return Ok(id.to_string());
        }
        if let Some(id) = result["channel_post"]["chat"]["id"].as_i64() {
            return Ok(id.to_string());
        }
    }

    Err(KittypawError::Skill(
        "채팅 ID를 찾을 수 없습니다. 봇에게 메시지를 보낸 후 다시 시도하세요".into(),
    ))
}

/// Send a text message via Telegram Bot API.
pub async fn send_message(token: &str, chat_id: &str, text: &str) -> Result<()> {
    let url = format!("https://api.telegram.org/bot{token}/sendMessage");
    let client = reqwest::Client::new();
    let resp = client
        .post(&url)
        .json(&serde_json::json!({
            "chat_id": chat_id,
            "text": text,
        }))
        .timeout(std::time::Duration::from_secs(10))
        .send()
        .await
        .map_err(|e| KittypawError::Skill(format!("Telegram 전송 실패: {e}")))?;

    let body: serde_json::Value = resp
        .json()
        .await
        .map_err(|e| KittypawError::Skill(format!("Telegram 응답 파싱 실패: {e}")))?;

    if body["ok"].as_bool() != Some(true) {
        return Err(KittypawError::Skill(format!(
            "Telegram 전송 실패: {}",
            body["description"].as_str().unwrap_or("unknown error")
        )));
    }

    Ok(())
}
