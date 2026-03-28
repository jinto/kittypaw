use kittypaw_core::permission::{
    FilePermissionRule, GlobalPath, NetworkPermissionRule, PermissionDecision, PermissionProfile,
};
use tauri::State;
use uuid::Uuid;

use crate::state::AppState;

// ── Permission popup response ──────────────────────────────────────────────

#[tauri::command]
pub fn respond_permission_request(
    request_id: String,
    decision: PermissionDecision,
    state: State<AppState>,
) -> Result<(), String> {
    let mut map = state.permission_requests.lock().unwrap();
    if let Some(sender) = map.remove(&request_id) {
        sender
            .send(decision)
            .map_err(|_| "Permission channel already closed".to_string())
    } else {
        Err(format!(
            "No pending permission request with id: {request_id}"
        ))
    }
}

// ── Permission profile query ───────────────────────────────────────────────

#[tauri::command]
pub fn get_permission_profile(
    workspace_id: String,
    state: State<AppState>,
) -> Result<PermissionProfile, String> {
    let store = state.store.lock().unwrap();
    store
        .load_permission_profile(&workspace_id)
        .map_err(|e| e.to_string())
}

// ── File rules ─────────────────────────────────────────────────────────────

#[tauri::command]
pub fn save_file_rule(
    workspace_id: String,
    path_pattern: String,
    is_exception: bool,
    can_read: bool,
    can_write: bool,
    can_delete: bool,
    state: State<AppState>,
) -> Result<String, String> {
    let id = Uuid::new_v4().to_string();
    let rule = FilePermissionRule {
        id: id.clone(),
        workspace_id,
        path_pattern,
        is_exception,
        can_read,
        can_write,
        can_delete,
    };
    let store = state.store.lock().unwrap();
    store.save_file_rule(&rule).map_err(|e| e.to_string())?;
    Ok(id)
}

#[tauri::command]
pub fn delete_file_rule(rule_id: String, state: State<AppState>) -> Result<(), String> {
    let store = state.store.lock().unwrap();
    store.delete_file_rule(&rule_id).map_err(|e| e.to_string())
}

// ── Network rules ──────────────────────────────────────────────────────────

#[tauri::command]
pub fn save_network_rule(
    workspace_id: String,
    domain_pattern: String,
    allowed_methods: Vec<kittypaw_core::permission::HttpMethod>,
    state: State<AppState>,
) -> Result<String, String> {
    let id = Uuid::new_v4().to_string();
    let rule = NetworkPermissionRule {
        id: id.clone(),
        workspace_id,
        domain_pattern,
        allowed_methods,
    };
    let store = state.store.lock().unwrap();
    store.save_network_rule(&rule).map_err(|e| e.to_string())?;
    Ok(id)
}

#[tauri::command]
pub fn delete_network_rule(rule_id: String, state: State<AppState>) -> Result<(), String> {
    let store = state.store.lock().unwrap();
    store
        .delete_network_rule(&rule_id)
        .map_err(|e| e.to_string())
}

// ── Global paths ───────────────────────────────────────────────────────────

#[tauri::command]
pub fn save_global_path(
    path: String,
    access_type: kittypaw_core::permission::AccessType,
    state: State<AppState>,
) -> Result<String, String> {
    let id = Uuid::new_v4().to_string();
    let global_path = GlobalPath {
        id: id.clone(),
        path,
        access_type,
    };
    let store = state.store.lock().unwrap();
    store
        .save_global_path(&global_path)
        .map_err(|e| e.to_string())?;
    Ok(id)
}

#[tauri::command]
pub fn delete_global_path(id: String, state: State<AppState>) -> Result<(), String> {
    let store = state.store.lock().unwrap();
    store.delete_global_path(&id).map_err(|e| e.to_string())
}
