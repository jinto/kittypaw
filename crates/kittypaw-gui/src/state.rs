use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use kittypaw_core::permission::PermissionDecision;
use kittypaw_store::Store;
use kittypaw_workspace::{FileIndexer, FilePermissionChecker, WorkspaceManager};

pub struct AppState {
    pub store: Arc<Mutex<Store>>,
    pub api_key: Arc<Mutex<String>>,
    pub workspace_manager: Arc<Mutex<WorkspaceManager>>,
    pub file_indexer: Arc<Mutex<Option<FileIndexer>>>,
    #[allow(dead_code)] // TODO: wire to agent_loop when permission integration is complete
    pub permission_checker: Arc<Mutex<FilePermissionChecker>>,
    pub permission_requests:
        Arc<Mutex<HashMap<String, tokio::sync::oneshot::Sender<PermissionDecision>>>>,
    pub packages_dir: PathBuf,
}

impl AppState {
    pub fn new(store: Store, api_key: String, packages_dir: PathBuf) -> Self {
        Self {
            store: Arc::new(Mutex::new(store)),
            api_key: Arc::new(Mutex::new(api_key)),
            workspace_manager: Arc::new(Mutex::new(WorkspaceManager::new())),
            file_indexer: Arc::new(Mutex::new(None)),
            permission_checker: Arc::new(Mutex::new(FilePermissionChecker::permissive())),
            permission_requests: Arc::new(Mutex::new(HashMap::new())),
            packages_dir,
        }
    }
}
