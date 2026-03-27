pub mod dashboard;
pub mod routes;

use axum::{Router, routing::get};
use oochy_core::metrics::Metrics;
use std::net::SocketAddr;
use tracing::info;

/// WebDashboard serves an embedded HTML dashboard over HTTP.
pub struct WebDashboard {
    db_path: String,
    addr: SocketAddr,
    metrics: Metrics,
}

impl WebDashboard {
    pub fn new(db_path: impl Into<String>, addr: SocketAddr) -> Self {
        Self {
            db_path: db_path.into(),
            addr,
            metrics: Metrics::new(),
        }
    }

    pub fn with_metrics(mut self, metrics: Metrics) -> Self {
        self.metrics = metrics;
        self
    }

    pub async fn serve(self) -> Result<(), Box<dyn std::error::Error>> {
        let db_path = self.db_path.clone();

        let app = Router::new()
            .route("/api/health", get(routes::health))
            .route("/api/metrics", get(routes::get_metrics))
            .route("/api/agents", get({
                let db = db_path.clone();
                move || routes::list_agents(db)
            }))
            .route("/api/agents/:id/conversations", get({
                let db = db_path.clone();
                move |path| routes::get_conversations(db, path)
            }))
            .fallback(dashboard::static_handler)
            .with_state(self.metrics);

        info!("Web dashboard listening on http://{}", self.addr);
        let listener = tokio::net::TcpListener::bind(self.addr).await?;
        axum::serve(listener, app).await?;
        Ok(())
    }
}
