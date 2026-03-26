use tracing_subscriber::EnvFilter;

mod store;

fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .init();

    println!("oochy v{}", env!("CARGO_PKG_VERSION"));
    println!("Usage: echo '{{\"type\":\"web_chat\",\"payload\":{{\"text\":\"hello\"}}}}' | oochy");
}
