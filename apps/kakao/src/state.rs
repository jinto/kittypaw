use std::sync::Arc;
use std::time::Duration;

use axum::extract::ws::Message;
use dashmap::DashMap;
use moka::future::Cache;
use tokio::sync::mpsc;

use crate::store::{SqliteStore, Store};
use crate::types::Config;

pub type WsSender = mpsc::UnboundedSender<Message>;

pub struct AppState {
    pub store: Arc<dyn Store>,
    pub sessions: Arc<DashMap<String, WsSender>>,
    pub pair_codes: Cache<String, String>,
    pub paired_markers: Cache<String, bool>,
    pub config: Arc<Config>,
    pub http_client: reqwest::Client,
}

impl AppState {
    pub fn new(config: Config) -> anyhow::Result<Self> {
        let store = SqliteStore::new(&config.database_path)?;

        let pair_codes = Cache::builder()
            .time_to_live(Duration::from_secs(300)) // 5 minutes
            .build();

        let paired_markers = Cache::builder()
            .time_to_live(Duration::from_secs(600)) // 10 minutes
            .build();

        let http_client = reqwest::Client::builder()
            .timeout(Duration::from_secs(30))
            .redirect(reqwest::redirect::Policy::none())
            .build()?;

        Ok(Self {
            store: Arc::new(store),
            sessions: Arc::new(DashMap::new()),
            pair_codes,
            paired_markers,
            config: Arc::new(config),
            http_client,
        })
    }
}
