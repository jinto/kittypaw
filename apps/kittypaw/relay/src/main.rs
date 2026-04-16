use std::time::Duration;

use tokio::net::TcpListener;
use tokio::signal;
use tracing::{info, warn};
use tracing_subscriber::EnvFilter;

use kittypaw_relay::routes;
use kittypaw_relay::state::AppState;
use kittypaw_relay::types::Config;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .init();

    let config = Config::from_env();
    if config.webhook_secret.is_empty() {
        warn!("WEBHOOK_SECRET is empty — all webhook/admin requests will be rejected");
    }
    let bind_addr = config.bind_addr.clone();

    let state = AppState::new(config)?;

    // Pending callback sweeper: clean up entries older than 10 minutes, every 60 seconds
    let sweeper_store = state.store.clone();
    tokio::spawn(async move {
        let mut interval = tokio::time::interval(Duration::from_secs(60));
        interval.tick().await; // skip first immediate tick
        loop {
            interval.tick().await;
            match sweeper_store.cleanup_expired_pending(600).await {
                Ok(0) => {}
                Ok(n) => info!("sweeper: cleaned {n} expired pending callbacks"),
                Err(e) => warn!("sweeper error: {e}"),
            }
        }
    });

    let app = routes::router(state);

    let listener = TcpListener::bind(&bind_addr).await?;
    info!("relay listening on {bind_addr}");

    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await?;

    info!("relay shut down");
    Ok(())
}

async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install SIGTERM handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => info!("received Ctrl+C, shutting down"),
        _ = terminate => info!("received SIGTERM, shutting down"),
    }
}
