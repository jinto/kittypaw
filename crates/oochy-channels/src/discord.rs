use async_trait::async_trait;
use oochy_core::error::{OochyError, Result};
use oochy_core::types::{Event, EventType};
use serenity::all::{ChannelId, Context, EventHandler, GatewayIntents, Message, Ready};
use serenity::Client;
use serde_json::json;
use tokio::sync::mpsc;
use tracing::{error, info};

use crate::channel::Channel;

pub struct DiscordChannel {
    bot_token: String,
}

impl DiscordChannel {
    pub fn new(bot_token: impl Into<String>) -> Self {
        Self {
            bot_token: bot_token.into(),
        }
    }
}

struct DiscordHandler {
    event_tx: mpsc::Sender<Event>,
}

#[async_trait]
impl EventHandler for DiscordHandler {
    async fn ready(&self, _ctx: Context, ready: Ready) {
        info!("Discord bot connected as {}", ready.user.name);
    }

    async fn message(&self, _ctx: Context, msg: Message) {
        // Ignore messages from bots (including self)
        if msg.author.bot {
            return;
        }

        let channel_id = msg.channel_id.get().to_string();
        let text = msg.content.clone();
        let from_name = msg.author.name.clone();
        let guild_id = msg.guild_id.map(|g| g.get().to_string());

        let event = Event {
            event_type: EventType::Discord,
            payload: json!({
                "channel_id": channel_id,
                "text": text,
                "from_name": from_name,
                "guild_id": guild_id,
            }),
        };

        if self.event_tx.send(event).await.is_err() {
            info!("Event receiver dropped, stopping Discord handler");
        }
    }
}

#[async_trait]
impl Channel for DiscordChannel {
    async fn start(&self, event_tx: mpsc::Sender<Event>) -> Result<()> {
        info!("Starting Discord channel gateway");

        let intents = GatewayIntents::GUILD_MESSAGES
            | GatewayIntents::DIRECT_MESSAGES
            | GatewayIntents::MESSAGE_CONTENT;

        let handler = DiscordHandler { event_tx };

        let mut client = Client::builder(&self.bot_token, intents)
            .event_handler(handler)
            .await
            .map_err(|e| OochyError::Config(format!("Failed to create Discord client: {}", e)))?;

        client.start().await.map_err(|e| {
            error!("Discord client error: {}", e);
            OochyError::Llm(format!("Discord client error: {}", e))
        })?;

        Ok(())
    }

    async fn send_response(&self, agent_id: &str, response: &str) -> Result<()> {
        // agent_id is used as the Discord channel_id
        let channel_id: u64 = agent_id.parse().map_err(|_| {
            OochyError::Config(format!("Invalid Discord channel_id: {}", agent_id))
        })?;

        let http = serenity::http::Http::new(&self.bot_token);

        ChannelId::new(channel_id)
            .say(&http, response)
            .await
            .map_err(|e| OochyError::Llm(format!("Discord send message failed: {}", e)))?;

        Ok(())
    }

    fn name(&self) -> &str {
        "discord"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_discord_channel_name() {
        let ch = DiscordChannel::new("dummy_token");
        assert_eq!(ch.name(), "discord");
    }

    #[test]
    fn test_discord_event_payload_structure() {
        let event = Event {
            event_type: EventType::Discord,
            payload: json!({
                "channel_id": "123456789",
                "text": "Hello from Discord!",
                "from_name": "TestUser",
                "guild_id": "987654321",
            }),
        };
        assert_eq!(event.event_type, EventType::Discord);
        assert_eq!(event.payload["channel_id"], "123456789");
        assert_eq!(event.payload["text"], "Hello from Discord!");
        assert_eq!(event.payload["from_name"], "TestUser");
        assert_eq!(event.payload["guild_id"], "987654321");
    }

    #[test]
    fn test_discord_event_payload_dm() {
        // DM messages have no guild_id
        let event = Event {
            event_type: EventType::Discord,
            payload: json!({
                "channel_id": "111222333",
                "text": "Direct message",
                "from_name": "DMUser",
                "guild_id": null,
            }),
        };
        assert_eq!(event.event_type, EventType::Discord);
        assert!(event.payload["guild_id"].is_null());
    }

    #[test]
    fn test_invalid_channel_id_returns_error() {
        let ch = DiscordChannel::new("dummy_token");
        let result = tokio::runtime::Runtime::new()
            .unwrap()
            .block_on(ch.send_response("not_a_number", "hi"));
        assert!(result.is_err());
    }
}
