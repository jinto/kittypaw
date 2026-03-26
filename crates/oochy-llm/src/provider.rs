use async_trait::async_trait;
use oochy_core::error::Result;
use oochy_core::types::LlmMessage;

#[async_trait]
pub trait LlmProvider: Send + Sync {
    async fn generate(&self, messages: &[LlmMessage]) -> Result<String>;
}
