use tauri::State;
use tracing::error;

use crate::state::AppState;

pub const NS_SETTINGS: &str = "settings";
pub const KEY_API_KEY: &str = "api_key";

#[tauri::command]
pub fn get_settings(state: State<AppState>) -> String {
    let api_key = state.api_key.lock().expect("api_key mutex poisoned");
    if api_key.is_empty() {
        return String::new();
    }
    let len = api_key.len();
    let suffix = &api_key[len.saturating_sub(4)..];
    format!("sk-...{suffix}")
}

#[tauri::command]
pub fn save_api_key(api_key: String, state: State<AppState>) -> Result<(), String> {
    let store = state.store.lock().expect("store mutex poisoned");
    store
        .storage_set(NS_SETTINGS, KEY_API_KEY, &api_key)
        .map_err(|e| {
            error!("Failed to persist API key: {e}");
            format!("Failed to save API key: {e}")
        })?;
    drop(store);

    let mut key = state.api_key.lock().expect("api_key mutex poisoned");
    *key = api_key;
    Ok(())
}

/// Internal-only: get the real API key for backend use. Not a Tauri command.
pub fn get_api_key_internal(state: &State<AppState>) -> String {
    state
        .api_key
        .lock()
        .expect("api_key mutex poisoned")
        .clone()
}
