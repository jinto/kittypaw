use std::collections::HashMap;

use kittypaw_core::package::SkillPackage;
use kittypaw_core::package_manager::PackageManager;
use tauri::State;

use crate::state::AppState;

#[tauri::command]
pub fn list_packages(state: State<AppState>) -> Result<Vec<SkillPackage>, String> {
    let mgr = PackageManager::new(state.packages_dir.clone());
    mgr.list_installed().map_err(|e| e.to_string())
}

#[tauri::command]
pub fn get_package_config(
    id: String,
    state: State<AppState>,
) -> Result<HashMap<String, String>, String> {
    let mgr = PackageManager::new(state.packages_dir.clone());
    mgr.get_config_with_defaults(&id).map_err(|e| e.to_string())
}

#[tauri::command]
pub fn set_package_config(
    id: String,
    key: String,
    value: String,
    state: State<AppState>,
) -> Result<(), String> {
    let mgr = PackageManager::new(state.packages_dir.clone());
    mgr.set_config(&id, &key, &value).map_err(|e| e.to_string())
}

#[tauri::command]
pub fn uninstall_package(id: String, state: State<AppState>) -> Result<(), String> {
    let mgr = PackageManager::new(state.packages_dir.clone());
    mgr.uninstall_package(&id).map_err(|e| e.to_string())
}
