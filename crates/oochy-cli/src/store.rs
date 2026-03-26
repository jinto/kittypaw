use oochy_core::{
    error::{OochyError, Result},
    types::{AgentState, ConversationTurn, Role},
};
use rusqlite::{params, Connection};

pub struct Store {
    conn: Connection,
}

impl Store {
    pub fn open(path: &str) -> Result<Self> {
        let conn = Connection::open(path).map_err(|e| OochyError::Store(e.to_string()))?;

        conn.busy_timeout(std::time::Duration::from_millis(5000))
            .map_err(|e| OochyError::Store(e.to_string()))?;

        conn.execute_batch("PRAGMA journal_mode=WAL;")
            .map_err(|e| OochyError::Store(e.to_string()))?;

        let migration_sql = include_str!("migrations/001_init.sql");
        conn.execute_batch(migration_sql)
            .map_err(|e| OochyError::Store(e.to_string()))?;

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
            Err(e) => Err(OochyError::Store(e.to_string())),
        }
    }

    pub fn save_state(&self, state: &AgentState) -> Result<()> {
        let state_json = serde_json::to_string(state).map_err(OochyError::Json)?;

        self.conn
            .execute(
                "INSERT OR REPLACE INTO agents (agent_id, system_prompt, state_json, updated_at) \
                 VALUES (?1, ?2, ?3, datetime('now'))",
                params![state.agent_id, state.system_prompt, state_json],
            )
            .map_err(|e| OochyError::Store(e.to_string()))?;

        Ok(())
    }

    pub fn add_turn(&self, agent_id: &str, turn: &ConversationTurn) -> Result<()> {
        let role_str = serde_json::to_string(&turn.role)
            .map_err(OochyError::Json)?
            .trim_matches('"')
            .to_string();

        self.conn
            .execute(
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
            )
            .map_err(|e| OochyError::Store(e.to_string()))?;

        Ok(())
    }

    pub fn recent_turns(&self, agent_id: &str, n: usize) -> Result<Vec<ConversationTurn>> {
        let mut stmt = self
            .conn
            .prepare(
                "SELECT role, content, code, result, timestamp \
                 FROM conversations WHERE agent_id = ?1 \
                 ORDER BY timestamp DESC, id DESC LIMIT ?2",
            )
            .map_err(|e| OochyError::Store(e.to_string()))?;

        let mut turns: Vec<ConversationTurn> = stmt
            .query_map(params![agent_id, n as i64], |row| {
                let role_str: String = row.get(0)?;
                Ok((
                    role_str,
                    row.get::<_, String>(1)?,
                    row.get::<_, Option<String>>(2)?,
                    row.get::<_, Option<String>>(3)?,
                    row.get::<_, String>(4)?,
                ))
            })
            .map_err(|e| OochyError::Store(e.to_string()))?
            .collect::<rusqlite::Result<Vec<_>>>()
            .map_err(|e| OochyError::Store(e.to_string()))?
            .into_iter()
            .map(|(role_str, content, code, result, timestamp)| {
                let role = parse_role(&role_str);
                ConversationTurn {
                    role,
                    content,
                    code,
                    result,
                    timestamp,
                }
            })
            .collect();

        turns.reverse();
        Ok(turns)
    }

    fn recent_turns_all(&self, agent_id: &str) -> Result<Vec<ConversationTurn>> {
        let mut stmt = self
            .conn
            .prepare(
                "SELECT role, content, code, result, timestamp \
                 FROM conversations WHERE agent_id = ?1 \
                 ORDER BY timestamp ASC, id ASC",
            )
            .map_err(|e| OochyError::Store(e.to_string()))?;

        let turns: Vec<ConversationTurn> = stmt
            .query_map(params![agent_id], |row| {
                let role_str: String = row.get(0)?;
                Ok((
                    role_str,
                    row.get::<_, String>(1)?,
                    row.get::<_, Option<String>>(2)?,
                    row.get::<_, Option<String>>(3)?,
                    row.get::<_, String>(4)?,
                ))
            })
            .map_err(|e| OochyError::Store(e.to_string()))?
            .collect::<rusqlite::Result<Vec<_>>>()
            .map_err(|e| OochyError::Store(e.to_string()))?
            .into_iter()
            .map(|(role_str, content, code, result, timestamp)| {
                let role = parse_role(&role_str);
                ConversationTurn {
                    role,
                    content,
                    code,
                    result,
                    timestamp,
                }
            })
            .collect();

        Ok(turns)
    }
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
            "oochy_test_{}_{}.db",
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
}
