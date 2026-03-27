use async_trait::async_trait;
use oochy_core::error::Result;
use oochy_core::types::Event;
use tokio::sync::mpsc;

#[async_trait]
pub trait Channel: Send + Sync {
    async fn start(&self, event_tx: mpsc::Sender<Event>) -> Result<()>;
    async fn send_response(&self, agent_id: &str, response: &str) -> Result<()>;
    fn name(&self) -> &str;
}
