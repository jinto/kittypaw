// Prevents an additional console window on Windows in release.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use kittypaw_store::Store;
mod bundled_packages;
mod commands;
mod state;

use commands::chat::send_message;
use commands::packages::{
    get_package_config, list_packages, set_package_config, uninstall_package,
};
use commands::permission::{
    delete_file_rule, delete_global_path, delete_network_rule, get_permission_profile,
    respond_permission_request, save_file_rule, save_global_path, save_network_rule,
};
use commands::search::{search_files, semantic_search};
use commands::settings::{get_settings, save_api_key};
use commands::workspace::{
    approve_change, list_files, open_workspace, preview_change, read_file, reject_change,
};
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

    use commands::settings::{KEY_API_KEY, NS_SETTINGS};
    let persisted_key = store
        .storage_get(NS_SETTINGS, KEY_API_KEY)
        .ok()
        .flatten()
        .unwrap_or_default();

    let app_state = AppState::new(store, persisted_key, packages_dir);

    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_shell::init())
        .manage(app_state)
        .invoke_handler(tauri::generate_handler![
            send_message,
            get_settings,
            save_api_key,
            open_workspace,
            list_files,
            read_file,
            preview_change,
            approve_change,
            reject_change,
            search_files,
            semantic_search,
            respond_permission_request,
            get_permission_profile,
            save_file_rule,
            delete_file_rule,
            save_network_rule,
            delete_network_rule,
            save_global_path,
            delete_global_path,
            list_packages,
            get_package_config,
            set_package_config,
            uninstall_package,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
