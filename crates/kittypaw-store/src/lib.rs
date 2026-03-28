use kittypaw_core::{
    error::{KittypawError, Result},
    permission::{
        AccessType, FilePermissionRule, GlobalPath, HttpMethod, NetworkPermissionRule,
        PermissionProfile,
    },
    types::{AgentState, ConversationTurn, Role},
};
use rusqlite::{params, Connection};
use rusqlite_migration::{Migrations, M};

pub struct Store {
    conn: Connection,
}

fn migrations() -> Migrations<'static> {
    Migrations::new(vec![
        M::up(include_str!("migrations/001_init.sql")),
        M::up(include_str!("migrations/002_skill_storage.sql")),
        M::up(include_str!("migrations/003_workspaces.sql")),
        M::up(include_str!("migrations/004_permissions.sql")),
    ])
}

impl Store {
    pub fn open(path: &str) -> Result<Self> {
        let mut conn = Connection::open(path)?;

        conn.busy_timeout(std::time::Duration::from_millis(5000))?;

        conn.execute_batch("PRAGMA journal_mode=WAL;")?;

        migrations()
            .to_latest(&mut conn)
            .map_err(|e| KittypawError::Store(e.to_string()))?;

        Ok(Self { conn })
    }

    pub fn load_state(&self, agent_id: &str) -> Result<Option<AgentState>> {
        let result: rusqlite::Result<(String, String)> = self.conn.query_row(
            "SELECT system_prompt, state_json FROM agents WHERE agent_id = ?1",
            params![agent_id],
            |row| Ok((row.get(0)?, row.get(1)?)),
        );

        match result {
            Ok((system_prompt, _state_json)) => {
                let turns = self.recent_turns_all(agent_id)?;
                Ok(Some(AgentState {
                    agent_id: agent_id.to_string(),
                    system_prompt,
                    turns,
                }))
            }
            Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(KittypawError::from(e)),
        }
    }

    pub fn save_state(&self, state: &AgentState) -> Result<()> {
        let state_json = serde_json::to_string(state).map_err(KittypawError::Json)?;

        self.conn.execute(
            "INSERT OR REPLACE INTO agents (agent_id, system_prompt, state_json, updated_at) \
                 VALUES (?1, ?2, ?3, datetime('now'))",
            params![state.agent_id, state.system_prompt, state_json],
        )?;

        Ok(())
    }

    pub fn add_turn(&self, agent_id: &str, turn: &ConversationTurn) -> Result<()> {
        let role_str = serde_json::to_string(&turn.role)
            .map_err(KittypawError::Json)?
            .trim_matches('"')
            .to_string();

        self.conn.execute(
            "INSERT INTO conversations (agent_id, role, content, code, result, timestamp) \
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            params![
                agent_id,
                role_str,
                turn.content,
                turn.code,
                turn.result,
                turn.timestamp
            ],
        )?;

        Ok(())
    }

    #[allow(dead_code)]
    pub fn recent_turns(&self, agent_id: &str, n: usize) -> Result<Vec<ConversationTurn>> {
        let mut stmt = self.conn.prepare(
            "SELECT role, content, code, result, timestamp \
                 FROM conversations WHERE agent_id = ?1 \
                 ORDER BY timestamp DESC, id DESC LIMIT ?2",
        )?;

        let mut turns: Vec<ConversationTurn> = stmt
            .query_map(params![agent_id, n as i64], map_turn_row)?
            .collect::<rusqlite::Result<Vec<_>>>()?;

        turns.reverse();
        Ok(turns)
    }

    fn recent_turns_all(&self, agent_id: &str) -> Result<Vec<ConversationTurn>> {
        let mut stmt = self.conn.prepare(
            "SELECT role, content, code, result, timestamp \
                 FROM conversations WHERE agent_id = ?1 \
                 ORDER BY timestamp ASC, id ASC LIMIT 100",
        )?;

        let turns: Vec<ConversationTurn> = stmt
            .query_map(params![agent_id], map_turn_row)?
            .collect::<rusqlite::Result<Vec<_>>>()?;

        Ok(turns)
    }

    pub fn storage_get(&self, namespace: &str, key: &str) -> Result<Option<String>> {
        let result: rusqlite::Result<String> = self.conn.query_row(
            "SELECT value FROM skill_storage WHERE namespace = ?1 AND key = ?2",
            params![namespace, key],
            |row| row.get(0),
        );
        match result {
            Ok(value) => Ok(Some(value)),
            Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(KittypawError::from(e)),
        }
    }

    pub fn storage_set(&self, namespace: &str, key: &str, value: &str) -> Result<()> {
        self.conn.execute(
            "INSERT OR REPLACE INTO skill_storage (namespace, key, value) VALUES (?1, ?2, ?3)",
            params![namespace, key, value],
        )?;
        Ok(())
    }

    pub fn storage_delete(&self, namespace: &str, key: &str) -> Result<()> {
        self.conn.execute(
            "DELETE FROM skill_storage WHERE namespace = ?1 AND key = ?2",
            params![namespace, key],
        )?;
        Ok(())
    }

    pub fn storage_list(&self, namespace: &str) -> Result<Vec<String>> {
        let mut stmt = self
            .conn
            .prepare("SELECT key FROM skill_storage WHERE namespace = ?1")?;
        let keys: Vec<String> = stmt
            .query_map(params![namespace], |row| row.get(0))?
            .collect::<rusqlite::Result<Vec<_>>>()?;
        Ok(keys)
    }

    pub fn save_workspace(&self, id: &str, name: &str, root_path: &str) -> Result<()> {
        self.conn.execute(
            "INSERT OR REPLACE INTO workspaces (id, name, root_path, last_opened_at) \
                 VALUES (?1, ?2, ?3, datetime('now'))",
            params![id, name, root_path],
        )?;
        Ok(())
    }

    pub fn list_workspaces(&self) -> Result<Vec<(String, String, String)>> {
        let mut stmt = self
            .conn
            .prepare("SELECT id, name, root_path FROM workspaces ORDER BY last_opened_at DESC")?;
        let rows: Vec<(String, String, String)> = stmt
            .query_map([], |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)))?
            .collect::<rusqlite::Result<Vec<_>>>()?;
        Ok(rows)
    }

    pub fn update_last_opened(&self, id: &str) -> Result<()> {
        self.conn.execute(
            "UPDATE workspaces SET last_opened_at = datetime('now') WHERE id = ?1",
            params![id],
        )?;
        Ok(())
    }

    // ── Permission CRUD ────────────────────────────────────────────────────

    pub fn save_file_rule(&self, rule: &FilePermissionRule) -> Result<()> {
        self.conn.execute(
            "INSERT OR REPLACE INTO permission_file_rules \
                 (id, workspace_id, path_pattern, is_exception, can_read, can_write, can_delete) \
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)",
            params![
                rule.id,
                rule.workspace_id,
                rule.path_pattern,
                rule.is_exception as i32,
                rule.can_read as i32,
                rule.can_write as i32,
                rule.can_delete as i32,
            ],
        )?;
        Ok(())
    }

    pub fn list_file_rules(&self, workspace_id: &str) -> Result<Vec<FilePermissionRule>> {
        let mut stmt = self.conn.prepare(
            "SELECT id, workspace_id, path_pattern, is_exception, can_read, can_write, can_delete \
                 FROM permission_file_rules WHERE workspace_id = ?1",
        )?;

        let rules = stmt
            .query_map(params![workspace_id], |row| {
                Ok(FilePermissionRule {
                    id: row.get(0)?,
                    workspace_id: row.get(1)?,
                    path_pattern: row.get(2)?,
                    is_exception: row.get::<_, i32>(3)? != 0,
                    can_read: row.get::<_, i32>(4)? != 0,
                    can_write: row.get::<_, i32>(5)? != 0,
                    can_delete: row.get::<_, i32>(6)? != 0,
                })
            })?
            .collect::<rusqlite::Result<Vec<_>>>()?;

        Ok(rules)
    }

    pub fn delete_file_rule(&self, rule_id: &str) -> Result<()> {
        self.conn.execute(
            "DELETE FROM permission_file_rules WHERE id = ?1",
            params![rule_id],
        )?;
        Ok(())
    }

    pub fn save_network_rule(&self, rule: &NetworkPermissionRule) -> Result<()> {
        let methods_json =
            serde_json::to_string(&rule.allowed_methods).map_err(KittypawError::Json)?;
        self.conn.execute(
            "INSERT OR REPLACE INTO permission_network_rules \
                 (id, workspace_id, domain_pattern, allowed_methods) \
                 VALUES (?1, ?2, ?3, ?4)",
            params![
                rule.id,
                rule.workspace_id,
                rule.domain_pattern,
                methods_json
            ],
        )?;
        Ok(())
    }

    pub fn list_network_rules(&self, workspace_id: &str) -> Result<Vec<NetworkPermissionRule>> {
        let mut stmt = self.conn.prepare(
            "SELECT id, workspace_id, domain_pattern, allowed_methods \
                 FROM permission_network_rules WHERE workspace_id = ?1",
        )?;

        let rules = stmt
            .query_map(params![workspace_id], |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, String>(1)?,
                    row.get::<_, String>(2)?,
                    row.get::<_, String>(3)?,
                ))
            })?
            .collect::<rusqlite::Result<Vec<_>>>()?
            .into_iter()
            .map(|(id, workspace_id, domain_pattern, methods_json)| {
                let allowed_methods: Vec<HttpMethod> =
                    serde_json::from_str(&methods_json).unwrap_or_default();
                NetworkPermissionRule {
                    id,
                    workspace_id,
                    domain_pattern,
                    allowed_methods,
                }
            })
            .collect();

        Ok(rules)
    }

    pub fn delete_network_rule(&self, rule_id: &str) -> Result<()> {
        self.conn.execute(
            "DELETE FROM permission_network_rules WHERE id = ?1",
            params![rule_id],
        )?;
        Ok(())
    }

    pub fn save_global_path(&self, global_path: &GlobalPath) -> Result<()> {
        let access_type_str = match global_path.access_type {
            AccessType::Read => "read",
            AccessType::Write => "write",
        };
        self.conn.execute(
            "INSERT OR REPLACE INTO permission_global_paths (id, path, access_type) \
                 VALUES (?1, ?2, ?3)",
            params![global_path.id, global_path.path, access_type_str],
        )?;
        Ok(())
    }

    pub fn list_global_paths(&self) -> Result<Vec<GlobalPath>> {
        let mut stmt = self
            .conn
            .prepare("SELECT id, path, access_type FROM permission_global_paths")?;

        let paths = stmt
            .query_map([], |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, String>(1)?,
                    row.get::<_, String>(2)?,
                ))
            })?
            .collect::<rusqlite::Result<Vec<_>>>()?
            .into_iter()
            .map(|(id, path, access_type_str)| {
                let access_type = if access_type_str == "write" {
                    AccessType::Write
                } else {
                    AccessType::Read
                };
                GlobalPath {
                    id,
                    path,
                    access_type,
                }
            })
            .collect();

        Ok(paths)
    }

    pub fn delete_global_path(&self, id: &str) -> Result<()> {
        self.conn.execute(
            "DELETE FROM permission_global_paths WHERE id = ?1",
            params![id],
        )?;
        Ok(())
    }

    pub fn load_permission_profile(&self, workspace_id: &str) -> Result<PermissionProfile> {
        let file_rules = self.list_file_rules(workspace_id)?;
        let network_rules = self.list_network_rules(workspace_id)?;
        let global_paths = self.list_global_paths()?;
        Ok(PermissionProfile {
            workspace_id: workspace_id.to_string(),
            file_rules,
            network_rules,
            global_paths,
        })
    }
}

fn map_turn_row(row: &rusqlite::Row) -> rusqlite::Result<ConversationTurn> {
    let role_str: String = row.get(0)?;
    Ok(ConversationTurn {
        role: parse_role(&role_str),
        content: row.get(1)?,
        code: row.get(2)?,
        result: row.get(3)?,
        timestamp: row.get(4)?,
    })
}

fn parse_role(s: &str) -> Role {
    match s {
        "user" => Role::User,
        "assistant" => Role::Assistant,
        "system" => Role::System,
        _ => Role::User,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    fn temp_db_path() -> PathBuf {
        use std::sync::atomic::{AtomicU64, Ordering};
        static COUNTER: AtomicU64 = AtomicU64::new(0);
        let mut p = std::env::temp_dir();
        p.push(format!(
            "kittypaw_test_{}_{}.db",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        p
    }

    fn make_turn(role: Role, content: &str) -> ConversationTurn {
        ConversationTurn {
            role,
            content: content.to_string(),
            code: None,
            result: None,
            timestamp: chrono_now(),
        }
    }

    fn chrono_now() -> String {
        use std::time::{SystemTime, UNIX_EPOCH};
        let secs = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();
        format!("{}", secs)
    }

    #[test]
    fn test_open_creates_db() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap());
        assert!(store.is_ok(), "Store::open should succeed");
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_save_load_roundtrip() {
        let path = temp_db_path();
        let path_str = path.to_str().unwrap();
        let store = Store::open(path_str).unwrap();

        let mut state = AgentState::new("agent-1", "You are a helpful assistant.");
        state.add_turn(make_turn(Role::User, "Hello"));
        state.add_turn(make_turn(Role::Assistant, "Hi there!"));

        store.save_state(&state).unwrap();

        // Also persist the turns
        for turn in &state.turns {
            store.add_turn("agent-1", turn).unwrap();
        }

        let loaded = store.load_state("agent-1").unwrap();
        assert!(loaded.is_some());
        let loaded = loaded.unwrap();
        assert_eq!(loaded.agent_id, "agent-1");
        assert_eq!(loaded.system_prompt, "You are a helpful assistant.");
        assert_eq!(loaded.turns.len(), 2);
        assert_eq!(loaded.turns[0].content, "Hello");
        assert_eq!(loaded.turns[1].content, "Hi there!");

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_add_and_recent_turns() {
        let path = temp_db_path();
        let path_str = path.to_str().unwrap();
        let store = Store::open(path_str).unwrap();

        // Ensure the agent row exists first
        let state = AgentState::new("agent-2", "system prompt");
        store.save_state(&state).unwrap();

        for i in 0..5u32 {
            let turn = ConversationTurn {
                role: Role::User,
                content: format!("message {}", i),
                code: None,
                result: None,
                timestamp: format!("2024-01-01 00:00:{:02}", i),
            };
            store.add_turn("agent-2", &turn).unwrap();
        }

        let recent = store.recent_turns("agent-2", 3).unwrap();
        assert_eq!(recent.len(), 3);
        assert_eq!(recent[0].content, "message 2");
        assert_eq!(recent[1].content, "message 3");
        assert_eq!(recent[2].content, "message 4");

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_empty_state() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();
        let result = store.load_state("nonexistent-agent").unwrap();
        assert!(result.is_none());
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_wal_mode() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();
        let mode: String = store
            .conn
            .query_row("PRAGMA journal_mode", [], |row| row.get(0))
            .unwrap();
        assert_eq!(mode, "wal");
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_storage_set_and_get() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        store.storage_set("ns", "key1", "val1").unwrap();
        let v = store.storage_get("ns", "key1").unwrap();
        assert_eq!(v, Some("val1".to_string()));

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_storage_get_nonexistent() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        let v = store.storage_get("ns", "missing").unwrap();
        assert_eq!(v, None);

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_storage_delete() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        store.storage_set("ns", "k", "v").unwrap();
        store.storage_delete("ns", "k").unwrap();
        let v = store.storage_get("ns", "k").unwrap();
        assert_eq!(v, None);

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_storage_list() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        store.storage_set("ns", "a", "1").unwrap();
        store.storage_set("ns", "b", "2").unwrap();
        let mut keys = store.storage_list("ns").unwrap();
        keys.sort();
        assert_eq!(keys, vec!["a", "b"]);

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_storage_namespace_isolation() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        store.storage_set("ns1", "key", "v1").unwrap();
        store.storage_set("ns2", "key", "v2").unwrap();

        assert_eq!(
            store.storage_get("ns1", "key").unwrap(),
            Some("v1".to_string())
        );
        assert_eq!(
            store.storage_get("ns2", "key").unwrap(),
            Some("v2".to_string())
        );

        let _ = std::fs::remove_file(&path);
    }

    // ── Permission CRUD tests ──────────────────────────────────────────────

    #[test]
    fn test_file_rule_roundtrip() {
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        // Need a workspace row for the FK constraint.
        store.save_workspace("ws1", "Test WS", "/tmp/ws1").unwrap();

        let rule = FilePermissionRule {
            id: "r1".to_string(),
            workspace_id: "ws1".to_string(),
            path_pattern: "/src".to_string(),
            is_exception: false,
            can_read: true,
            can_write: false,
            can_delete: false,
        };
        store.save_file_rule(&rule).unwrap();

        let rules = store.list_file_rules("ws1").unwrap();
        assert_eq!(rules.len(), 1);
        assert_eq!(rules[0].path_pattern, "/src");
        assert!(rules[0].can_read);
        assert!(!rules[0].can_write);

        store.delete_file_rule("r1").unwrap();
        let rules = store.list_file_rules("ws1").unwrap();
        assert!(rules.is_empty());

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_network_rule_roundtrip() {
        use crate::HttpMethod;
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        store
            .save_workspace("ws2", "Test WS 2", "/tmp/ws2")
            .unwrap();

        let rule = NetworkPermissionRule {
            id: "n1".to_string(),
            workspace_id: "ws2".to_string(),
            domain_pattern: "api.example.com".to_string(),
            allowed_methods: vec![HttpMethod::Get, HttpMethod::Post],
        };
        store.save_network_rule(&rule).unwrap();

        let rules = store.list_network_rules("ws2").unwrap();
        assert_eq!(rules.len(), 1);
        assert_eq!(rules[0].domain_pattern, "api.example.com");
        assert_eq!(rules[0].allowed_methods.len(), 2);

        store.delete_network_rule("n1").unwrap();
        let rules = store.list_network_rules("ws2").unwrap();
        assert!(rules.is_empty());

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_global_path_roundtrip() {
        use crate::AccessType;
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        let gp = GlobalPath {
            id: "gp1".to_string(),
            path: "/global/shared".to_string(),
            access_type: AccessType::Read,
        };
        store.save_global_path(&gp).unwrap();

        let paths = store.list_global_paths().unwrap();
        assert_eq!(paths.len(), 1);
        assert_eq!(paths[0].path, "/global/shared");
        assert_eq!(paths[0].access_type, AccessType::Read);

        store.delete_global_path("gp1").unwrap();
        let paths = store.list_global_paths().unwrap();
        assert!(paths.is_empty());

        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn test_load_permission_profile() {
        use crate::{AccessType, HttpMethod};
        let path = temp_db_path();
        let store = Store::open(path.to_str().unwrap()).unwrap();

        store
            .save_workspace("ws3", "Test WS 3", "/tmp/ws3")
            .unwrap();

        store
            .save_file_rule(&FilePermissionRule {
                id: "fr1".to_string(),
                workspace_id: "ws3".to_string(),
                path_pattern: "/src".to_string(),
                is_exception: false,
                can_read: true,
                can_write: true,
                can_delete: false,
            })
            .unwrap();

        store
            .save_network_rule(&NetworkPermissionRule {
                id: "nr1".to_string(),
                workspace_id: "ws3".to_string(),
                domain_pattern: "*.example.com".to_string(),
                allowed_methods: vec![HttpMethod::Get],
            })
            .unwrap();

        store
            .save_global_path(&GlobalPath {
                id: "gp2".to_string(),
                path: "/shared".to_string(),
                access_type: AccessType::Read,
            })
            .unwrap();

        let profile = store.load_permission_profile("ws3").unwrap();
        assert_eq!(profile.workspace_id, "ws3");
        assert_eq!(profile.file_rules.len(), 1);
        assert_eq!(profile.network_rules.len(), 1);
        assert_eq!(profile.global_paths.len(), 1);

        let _ = std::fs::remove_file(&path);
    }
}
