use std::io::Read;

use tracing_subscriber::EnvFilter;

mod agent_loop;
mod store;

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .init();

    // Read event from stdin
    let mut input = String::new();
    if atty::is(atty::Stream::Stdin) {
        eprintln!("oochy v{}", env!("CARGO_PKG_VERSION"));
        eprintln!("Usage: echo '{{\"type\":\"web_chat\",\"payload\":{{\"text\":\"hello\"}}}}' | oochy");
        std::process::exit(0);
    }

    std::io::stdin().read_to_string(&mut input).expect("failed to read stdin");
    let input = input.trim();
    if input.is_empty() {
        eprintln!("Error: empty input");
        std::process::exit(1);
    }

    // Parse event
    let event: oochy_core::types::Event = match serde_json::from_str(input) {
        Ok(e) => e,
        Err(e) => {
            eprintln!("Error parsing event JSON: {e}");
            eprintln!("Expected: {{\"type\":\"web_chat\",\"payload\":{{\"text\":\"hello\"}}}}");
            std::process::exit(1);
        }
    };

    // Load config
    let config = oochy_core::config::Config::load().unwrap_or_else(|e| {
        eprintln!("Config error: {e}");
        std::process::exit(1);
    });

    if config.llm.api_key.is_empty() {
        eprintln!("Error: OOCHY_API_KEY not set. Export your Claude API key:");
        eprintln!("  export OOCHY_API_KEY=sk-ant-...");
        std::process::exit(1);
    }

    // Initialize components
    let provider = oochy_llm::claude::ClaudeProvider::new(
        config.llm.api_key.clone(),
        config.llm.model.clone(),
        config.llm.max_tokens,
    );

    let sandbox = oochy_sandbox::sandbox::Sandbox::new(
        config.sandbox.timeout_secs,
        config.sandbox.memory_limit_mb,
    );

    let db_path = std::env::var("OOCHY_DB_PATH").unwrap_or_else(|_| "oochy.db".into());
    let store = store::Store::open(&db_path).unwrap_or_else(|e| {
        eprintln!("Database error: {e}");
        std::process::exit(1);
    });

    // Run agent loop
    match agent_loop::run_agent_loop(event, &provider, &sandbox, &store).await {
        Ok(output) => {
            println!("{output}");
        }
        Err(e) => {
            eprintln!("Error: {e}");
            std::process::exit(1);
        }
    }
}
