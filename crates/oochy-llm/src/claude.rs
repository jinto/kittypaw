use async_trait::async_trait;
use oochy_core::error::{OochyError, Result};
use oochy_core::types::LlmMessage;
use serde::{Deserialize, Serialize};

use crate::provider::LlmProvider;
use crate::util::strip_code_fences;

pub struct ClaudeProvider {
    api_key: String,
    model: String,
    max_tokens: u32,
    client: reqwest::Client,
}

impl ClaudeProvider {
    pub fn new(api_key: String, model: String, max_tokens: u32) -> Self {
        Self {
            api_key,
            model,
            max_tokens,
            client: reqwest::Client::new(),
        }
    }
}

#[derive(Serialize)]
struct ClaudeRequest {
    model: String,
    max_tokens: u32,
    system: String,
    messages: Vec<ClaudeMessage>,
}

#[derive(Serialize)]
struct ClaudeMessage {
    role: String,
    content: String,
}

#[derive(Deserialize)]
struct ClaudeResponse {
    content: Vec<ContentBlock>,
}

#[derive(Deserialize)]
struct ContentBlock {
    text: Option<String>,
}

#[async_trait]
impl LlmProvider for ClaudeProvider {
    async fn generate(&self, messages: &[LlmMessage]) -> Result<String> {
        let system = messages
            .iter()
            .find(|m| m.role == oochy_core::types::Role::System)
            .map(|m| m.content.clone())
            .unwrap_or_default();

        let api_messages: Vec<ClaudeMessage> = messages
            .iter()
            .filter(|m| m.role != oochy_core::types::Role::System)
            .map(|m| ClaudeMessage {
                role: match m.role {
                    oochy_core::types::Role::User => "user".into(),
                    oochy_core::types::Role::Assistant => "assistant".into(),
                    oochy_core::types::Role::System => unreachable!(),
                },
                content: m.content.clone(),
            })
            .collect();

        let request = ClaudeRequest {
            model: self.model.clone(),
            max_tokens: self.max_tokens,
            system,
            messages: api_messages,
        };

        let mut retries = 0;
        let max_retries = 3;

        loop {
            let response = self
                .client
                .post("https://api.anthropic.com/v1/messages")
                .header("x-api-key", &self.api_key)
                .header("anthropic-version", "2023-06-01")
                .header("content-type", "application/json")
                .json(&request)
                .send()
                .await
                .map_err(|e| OochyError::Llm(format!("HTTP error: {e}")))?;

            let status = response.status();

            if status == 429 {
                retries += 1;
                if retries > max_retries {
                    return Err(OochyError::Llm("Rate limited after max retries".into()));
                }
                let delay = std::time::Duration::from_millis(1000 * 2u64.pow(retries));
                tracing::warn!("Rate limited, retrying in {:?}", delay);
                tokio::time::sleep(delay).await;
                continue;
            }

            if status.is_server_error() {
                retries += 1;
                if retries > max_retries {
                    return Err(OochyError::Llm(format!("Server error {status} after max retries")));
                }
                let delay = std::time::Duration::from_millis(1000 * 2u64.pow(retries));
                tracing::warn!("Server error {status}, retrying in {:?}", delay);
                tokio::time::sleep(delay).await;
                continue;
            }

            if !status.is_success() {
                let body = response.text().await.unwrap_or_default();
                return Err(OochyError::Llm(format!("API error {status}: {body}")));
            }

            let body: ClaudeResponse = response
                .json()
                .await
                .map_err(|e| OochyError::Llm(format!("Response parse error: {e}")))?;

            let text = body
                .content
                .into_iter()
                .filter_map(|b| b.text)
                .collect::<Vec<_>>()
                .join("");

            return Ok(strip_code_fences(&text));
        }
    }
}
