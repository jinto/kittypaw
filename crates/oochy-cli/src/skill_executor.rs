use oochy_core::error::{OochyError, Result};
use oochy_core::types::SkillCall;

/// Execute captured skill calls on the host side (outside sandbox).
/// Each skill call was captured by JS stubs inside QuickJS and is now
/// executed with real API calls after capability checking.
pub async fn execute_skill_calls(
    skill_calls: &[SkillCall],
    _config: &oochy_core::config::Config,
) -> Result<Vec<SkillResult>> {
    let mut results = Vec::new();

    for call in skill_calls {
        let result = execute_single_call(call).await;
        results.push(result);
    }

    Ok(results)
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct SkillResult {
    pub skill_name: String,
    pub method: String,
    pub success: bool,
    pub result: serde_json::Value,
    pub error: Option<String>,
}

async fn execute_single_call(call: &SkillCall) -> SkillResult {
    let result = match call.skill_name.as_str() {
        "Telegram" => execute_telegram(call).await,
        "Discord" => execute_discord(call).await,
        "Http" => execute_http(call).await,
        "Storage" => execute_storage(call).await,
        _ => Err(OochyError::CapabilityDenied(format!(
            "Unknown skill: {}",
            call.skill_name
        ))),
    };

    match result {
        Ok(value) => SkillResult {
            skill_name: call.skill_name.clone(),
            method: call.method.clone(),
            success: true,
            result: value,
            error: None,
        },
        Err(e) => SkillResult {
            skill_name: call.skill_name.clone(),
            method: call.method.clone(),
            success: false,
            result: serde_json::Value::Null,
            error: Some(e.to_string()),
        },
    }
}

async fn execute_telegram(call: &SkillCall) -> Result<serde_json::Value> {
    let bot_token = std::env::var("OOCHY_TELEGRAM_TOKEN")
        .map_err(|_| OochyError::Config("OOCHY_TELEGRAM_TOKEN not set".into()))?;

    let client = reqwest::Client::new();

    match call.method.as_str() {
        "sendMessage" => {
            let chat_id = call.args.first().and_then(|v| v.as_str()).unwrap_or("");
            let text = call.args.get(1).and_then(|v| v.as_str()).unwrap_or("");

            let url = format!("https://api.telegram.org/bot{bot_token}/sendMessage");
            let resp = client
                .post(&url)
                .json(&serde_json::json!({
                    "chat_id": chat_id,
                    "text": text,
                }))
                .send()
                .await
                .map_err(|e| OochyError::Llm(format!("Telegram API error: {e}")))?;

            let body: serde_json::Value = resp
                .json()
                .await
                .map_err(|e| OochyError::Llm(format!("Telegram response parse error: {e}")))?;

            Ok(body)
        }
        "sendPhoto" => {
            let chat_id = call.args.first().and_then(|v| v.as_str()).unwrap_or("");
            let photo_url = call.args.get(1).and_then(|v| v.as_str()).unwrap_or("");

            let url = format!("https://api.telegram.org/bot{bot_token}/sendPhoto");
            let resp = client
                .post(&url)
                .json(&serde_json::json!({
                    "chat_id": chat_id,
                    "photo": photo_url,
                }))
                .send()
                .await
                .map_err(|e| OochyError::Llm(format!("Telegram API error: {e}")))?;

            let body: serde_json::Value = resp.json().await.map_err(|e| OochyError::Llm(format!("{e}")))?;
            Ok(body)
        }
        _ => Err(OochyError::CapabilityDenied(format!(
            "Unknown Telegram method: {}",
            call.method
        ))),
    }
}

async fn execute_discord(_call: &SkillCall) -> Result<serde_json::Value> {
    // Discord skill execution will be wired through serenity's HTTP client
    // For now, stub that logs the call
    tracing::info!("Discord skill call (stub): {}.{}", _call.skill_name, _call.method);
    Ok(serde_json::json!({"ok": true, "stub": true}))
}

async fn execute_http(call: &SkillCall) -> Result<serde_json::Value> {
    let client = reqwest::Client::new();
    let url = call.args.first().and_then(|v| v.as_str()).unwrap_or("");

    if url.is_empty() {
        return Err(OochyError::Sandbox("Http: URL is required".into()));
    }

    let resp = match call.method.as_str() {
        "get" => client.get(url).send().await,
        "post" => {
            let body = call.args.get(1).cloned().unwrap_or(serde_json::Value::Null);
            client.post(url).json(&body).send().await
        }
        "put" => {
            let body = call.args.get(1).cloned().unwrap_or(serde_json::Value::Null);
            client.put(url).json(&body).send().await
        }
        "delete" => client.delete(url).send().await,
        _ => {
            return Err(OochyError::CapabilityDenied(format!(
                "Unknown Http method: {}",
                call.method
            )))
        }
    }
    .map_err(|e| OochyError::Llm(format!("HTTP error: {e}")))?;

    let status = resp.status().as_u16();
    let body: serde_json::Value = resp
        .json()
        .await
        .unwrap_or(serde_json::Value::String("(non-JSON response)".into()));

    Ok(serde_json::json!({
        "status": status,
        "body": body,
    }))
}

async fn execute_storage(call: &SkillCall) -> Result<serde_json::Value> {
    // Storage will be backed by SQLite in a future phase
    tracing::info!("Storage skill call (stub): {}.{}", call.skill_name, call.method);
    Ok(serde_json::json!({"ok": true, "stub": true}))
}
