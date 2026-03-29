#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod bundled_packages;
mod components;
mod state;

use std::sync::Arc;

use kittypaw_llm::claude::ClaudeProvider;
use kittypaw_llm::registry::LlmRegistry;
use kittypaw_store::Store;
use state::AppState;

fn main() {
    let data_dir = dirs_next::home_dir()
        .map(|p| p.join(".kittypaw"))
        .unwrap_or_else(|| std::path::PathBuf::from(".kittypaw"));

    let db_path = data_dir.join("kittypaw.db");
    if let Some(parent) = db_path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }

    let packages_dir = data_dir.join("packages");
    let _ = std::fs::create_dir_all(&packages_dir);
    bundled_packages::install_bundled_packages(&packages_dir);

    let store =
        Store::open(db_path.to_str().unwrap_or("kittypaw.db")).expect("Failed to open database");

    let persisted_key = kittypaw_core::secrets::get_secret("settings", "api_key")
        .ok()
        .flatten()
        .unwrap_or_default();

    let mut llm_registry = LlmRegistry::new();
    if !persisted_key.is_empty() {
        llm_registry.register(
            "claude-sonnet",
            Arc::new(ClaudeProvider::new(
                persisted_key.clone(),
                "claude-sonnet-4-20250514".into(),
                4096,
            )),
        );
    }

    let app_state = AppState::new(store, persisted_key, packages_dir, llm_registry);

    dioxus::LaunchBuilder::desktop()
        .with_context(app_state)
        .launch(components::app::App);
}
