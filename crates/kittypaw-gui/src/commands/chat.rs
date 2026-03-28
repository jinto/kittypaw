use std::sync::Arc;

use kittypaw_core::config::{Config, SandboxConfig};

const DEFAULT_MODEL: &str = "claude-sonnet-4-20250514";
use kittypaw_core::permission::{PermissionDecision, PermissionRequest};
use kittypaw_core::types::{Event, EventType};
use kittypaw_llm::claude::ClaudeProvider;
use kittypaw_sandbox::sandbox::Sandbox;
use tauri::{AppHandle, Emitter, State};

use crate::commands::settings::get_api_key_internal;
use crate::state::AppState;

/// Extract `@filename` references from `message`, read each file from the active
/// workspace, and prepend their contents as context before the user message.
fn build_message_with_context(
    message: &str,
    state: &State<AppState>,
    active_workspace_id: Option<&str>,
) -> String {
    // Collect @filename tokens
    let mentions: Vec<&str> = message
        .split_whitespace()
        .filter(|tok| tok.starts_with('@') && tok.len() > 1)
        .collect();

    if mentions.is_empty() || active_workspace_id.is_none() {
        return message.to_string();
    }

    let ws_id = active_workspace_id.unwrap();
    let mgr = state.workspace_manager.lock().unwrap();

    let mut context_blocks: Vec<String> = Vec::new();
    for mention in &mentions {
        let rel_path = &mention[1..]; // strip leading '@'
                                      // Reject empty or null-byte paths
        if rel_path.is_empty() || rel_path.contains('\0') {
            continue;
        }
        // Only read from the active workspace
        if let Ok(content) = mgr.read_file(ws_id, rel_path) {
            context_blocks.push(format!(
                "=== File: {rel_path} ===\n{content}\n=== End of {rel_path} ==="
            ));
        }
    }

    if context_blocks.is_empty() {
        return message.to_string();
    }

    format!("{}\n\n{}", context_blocks.join("\n\n"), message)
}

#[tauri::command]
pub async fn send_message(
    message: String,
    app_handle: AppHandle,
    state: State<'_, AppState>,
) -> Result<String, String> {
    let api_key = get_api_key_internal(&state);

    if api_key.is_empty() {
        return Err("API key not configured. Please set your API key in Settings.".to_string());
    }

    // Use active workspace for @filename scoping
    let active_ws_id: Option<String> = {
        let mgr = state.workspace_manager.lock().unwrap();
        let id = mgr.workspaces_iter().next().map(|(id, _)| id.to_string());
        id
    };
    let full_message = build_message_with_context(&message, &state, active_ws_id.as_deref());

    let provider = ClaudeProvider::new(api_key, DEFAULT_MODEL.to_string(), 4096);

    let sandbox_config = SandboxConfig {
        timeout_secs: 30,
        memory_limit_mb: 128,
        allowed_paths: vec![],
        allowed_hosts: vec![],
    };
    let sandbox = Sandbox::new_threaded(sandbox_config);

    let event = Event {
        event_type: EventType::Desktop,
        payload: serde_json::json!({ "text": full_message }),
    };

    let config = Config::default();

    let app_handle_clone = app_handle.clone();
    let on_token: Option<Arc<dyn Fn(String) + Send + Sync>> =
        Some(Arc::new(move |token: String| {
            let _ = app_handle_clone.emit("llm-stream", token);
        }));

    let store_arc = state.store.clone();
    let permission_requests = state.permission_requests.clone();
    let app_handle_for_perm = app_handle.clone();

    let on_permission_request: Option<
        Arc<
            dyn Fn(PermissionRequest) -> tokio::sync::oneshot::Receiver<PermissionDecision>
                + Send
                + Sync,
        >,
    > = Some(Arc::new(move |req: PermissionRequest| {
        let (tx, rx) = tokio::sync::oneshot::channel();
        let request_id = req.request_id.clone();
        {
            let mut map = permission_requests.lock().unwrap();
            map.insert(request_id, tx);
        }
        let _ = app_handle_for_perm.emit("permission-request", &req);
        rx
    }));

    // agent_loop borrows Store via Arc<Mutex<>>, so run on a blocking thread.
    // Pass store_arc directly — agent_loop locks per-operation internally.
    let result = tokio::task::spawn_blocking(move || {
        let rt = tokio::runtime::Handle::current();
        rt.block_on(kittypaw_cli::agent_loop::run_agent_loop(
            event,
            &provider,
            &sandbox,
            store_arc,
            &config,
            on_token,
            on_permission_request,
        ))
    })
    .await
    .map_err(|e| format!("Task join error: {e}"))?
    .map_err(|e| e.to_string())?;

    Ok(result)
}
