use async_trait::async_trait;
use kittypaw_core::error::Result;
use kittypaw_core::types::LlmMessage;
use serde::{Deserialize, Serialize};
use std::sync::Arc;

/// Token usage from a single LLM API call.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct TokenUsage {
    pub input_tokens: u64,
    pub output_tokens: u64,
    pub model: String,
}

/// Response from an LLM provider, wrapping content and optional usage metadata.
#[derive(Debug, Clone)]
pub struct LlmResponse {
    pub content: String,
    pub usage: Option<TokenUsage>,
}

impl LlmResponse {
    /// Create a response with no usage info (for local/Ollama providers).
    pub fn text_only(content: String) -> Self {
        Self {
            content,
            usage: None,
        }
    }
}

#[async_trait]
pub trait LlmProvider: Send + Sync {
    async fn generate(&self, messages: &[LlmMessage]) -> Result<LlmResponse>;

    /// Maximum context window in tokens. Used for prompt budget calculation.
    fn context_window(&self) -> usize {
        8_192
    }

    /// Maximum output tokens reserved for the model's response.
    fn max_tokens(&self) -> usize {
        4_096
    }

    /// Optional streaming generation. Default implementation collects full response.
    async fn generate_stream(
        &self,
        messages: &[LlmMessage],
        on_token: Arc<dyn Fn(String) + Send + Sync>,
    ) -> Result<LlmResponse> {
        let result = self.generate(messages).await?;
        on_token(result.content.clone());
        Ok(result)
    }
}
