use anyhow::Result;
use async_trait::async_trait;
use rusqlite::Connection;
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};
use tokio::sync::Mutex;

use crate::types::{PendingContext, RateLimitResult, Stats};

// ── Store trait ──

#[async_trait]
pub trait Store: Send + Sync {
    async fn token_exists(&self, token: &str) -> Result<bool>;
    async fn put_token(&self, token: &str) -> Result<()>;
    async fn get_user_mapping(&self, kakao_id: &str) -> Result<Option<String>>;
    async fn put_user_mapping(&self, kakao_id: &str, token: &str) -> Result<()>;
    async fn delete_user_mapping(&self, kakao_id: &str) -> Result<()>;
    async fn get_killswitch(&self) -> Result<bool>;
    async fn set_killswitch(&self, enabled: bool) -> Result<()>;
    async fn put_pending(&self, action_id: &str, ctx: &PendingContext) -> Result<()>;
    async fn take_pending(&self, action_id: &str) -> Result<Option<PendingContext>>;
    async fn check_rate_limit(&self, daily_limit: u64, monthly_limit: u64) -> Result<RateLimitResult>;
    async fn get_stats(&self) -> Result<Stats>;
    async fn cleanup_expired_pending(&self, max_age_secs: i64) -> Result<u64>;
}

// ── SqliteStore ──

pub struct SqliteStore {
    conn: Arc<Mutex<Connection>>,
}

impl SqliteStore {
    pub fn new(path: &str) -> Result<Self> {
        let conn = Connection::open(path)?;
        conn.pragma_update(None, "journal_mode", "WAL")?;
        conn.pragma_update(None, "busy_timeout", 5000)?;

        conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS tokens (
                token TEXT PRIMARY KEY
            );
            CREATE TABLE IF NOT EXISTS user_mappings (
                kakao_id TEXT PRIMARY KEY,
                token TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS killswitch (
                id INTEGER PRIMARY KEY CHECK(id = 1),
                enabled INTEGER NOT NULL DEFAULT 0
            );
            CREATE TABLE IF NOT EXISTS pending_callbacks (
                action_id TEXT PRIMARY KEY,
                callback_url TEXT NOT NULL,
                user_id TEXT NOT NULL,
                created_at INTEGER NOT NULL
            );
            CREATE TABLE IF NOT EXISTS rate_counters (
                key TEXT PRIMARY KEY,
                count INTEGER NOT NULL DEFAULT 0
            );
            INSERT OR IGNORE INTO killswitch (id, enabled) VALUES (1, 0);",
        )?;

        Ok(Self {
            conn: Arc::new(Mutex::new(conn)),
        })
    }
}

fn now_epoch() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64
}

fn today_key() -> String {
    let now = chrono_free_date();
    format!("d:{}", now)
}

fn month_key() -> String {
    let now = chrono_free_date();
    format!("m:{}", &now[..7])
}

/// Returns YYYY-MM-DD without chrono dependency.
fn chrono_free_date() -> String {
    let secs = now_epoch();
    let days = secs / 86400;
    // Civil date from Unix days (algorithm from Howard Hinnant)
    let z = days + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = (yoe as i64) + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };
    format!("{:04}-{:02}-{:02}", y, m, d)
}

#[async_trait]
impl Store for SqliteStore {
    async fn token_exists(&self, token: &str) -> Result<bool> {
        let conn = self.conn.clone();
        let token = token.to_string();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            let exists: bool = conn.query_row(
                "SELECT EXISTS(SELECT 1 FROM tokens WHERE token = ?1)",
                [&token],
                |row| row.get(0),
            )?;
            Ok(exists)
        })
        .await?
    }

    async fn put_token(&self, token: &str) -> Result<()> {
        let conn = self.conn.clone();
        let token = token.to_string();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            conn.execute(
                "INSERT OR IGNORE INTO tokens (token) VALUES (?1)",
                [&token],
            )?;
            Ok(())
        })
        .await?
    }

    async fn get_user_mapping(&self, kakao_id: &str) -> Result<Option<String>> {
        let conn = self.conn.clone();
        let kakao_id = kakao_id.to_string();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            let result = conn.query_row(
                "SELECT token FROM user_mappings WHERE kakao_id = ?1",
                [&kakao_id],
                |row| row.get::<_, String>(0),
            );
            match result {
                Ok(token) => Ok(Some(token)),
                Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
                Err(e) => Err(e.into()),
            }
        })
        .await?
    }

    async fn put_user_mapping(&self, kakao_id: &str, token: &str) -> Result<()> {
        let conn = self.conn.clone();
        let kakao_id = kakao_id.to_string();
        let token = token.to_string();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            conn.execute(
                "INSERT OR REPLACE INTO user_mappings (kakao_id, token) VALUES (?1, ?2)",
                [&kakao_id, &token],
            )?;
            Ok(())
        })
        .await?
    }

    async fn delete_user_mapping(&self, kakao_id: &str) -> Result<()> {
        let conn = self.conn.clone();
        let kakao_id = kakao_id.to_string();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            conn.execute(
                "DELETE FROM user_mappings WHERE kakao_id = ?1",
                [&kakao_id],
            )?;
            Ok(())
        })
        .await?
    }

    async fn get_killswitch(&self) -> Result<bool> {
        let conn = self.conn.clone();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            let enabled: i32 = conn.query_row(
                "SELECT enabled FROM killswitch WHERE id = 1",
                [],
                |row| row.get(0),
            )?;
            Ok(enabled != 0)
        })
        .await?
    }

    async fn set_killswitch(&self, enabled: bool) -> Result<()> {
        let conn = self.conn.clone();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            conn.execute(
                "UPDATE killswitch SET enabled = ?1 WHERE id = 1",
                [enabled as i32],
            )?;
            Ok(())
        })
        .await?
    }

    async fn put_pending(&self, action_id: &str, ctx: &PendingContext) -> Result<()> {
        let conn = self.conn.clone();
        let action_id = action_id.to_string();
        let ctx = ctx.clone();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            conn.execute(
                "INSERT OR REPLACE INTO pending_callbacks (action_id, callback_url, user_id, created_at)
                 VALUES (?1, ?2, ?3, ?4)",
                rusqlite::params![action_id, ctx.callback_url, ctx.user_id, ctx.created_at],
            )?;
            Ok(())
        })
        .await?
    }

    async fn take_pending(&self, action_id: &str) -> Result<Option<PendingContext>> {
        let conn = self.conn.clone();
        let action_id = action_id.to_string();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            // Atomic claim: SELECT then DELETE in sequence (single-threaded via Mutex)
            let result = conn.query_row(
                "SELECT callback_url, user_id, created_at FROM pending_callbacks WHERE action_id = ?1",
                [&action_id],
                |row| {
                    Ok(PendingContext {
                        callback_url: row.get(0)?,
                        user_id: row.get(1)?,
                        created_at: row.get(2)?,
                    })
                },
            );
            match result {
                Ok(ctx) => {
                    conn.execute(
                        "DELETE FROM pending_callbacks WHERE action_id = ?1",
                        [&action_id],
                    )?;
                    Ok(Some(ctx))
                }
                Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
                Err(e) => Err(e.into()),
            }
        })
        .await?
    }

    async fn check_rate_limit(&self, daily_limit: u64, monthly_limit: u64) -> Result<RateLimitResult> {
        let conn = self.conn.clone();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            let dk = today_key();
            let mk = month_key();

            // Ensure rows exist
            conn.execute(
                "INSERT OR IGNORE INTO rate_counters (key, count) VALUES (?1, 0)",
                [&dk],
            )?;
            conn.execute(
                "INSERT OR IGNORE INTO rate_counters (key, count) VALUES (?1, 0)",
                [&mk],
            )?;

            // Read current
            let daily: u64 = conn.query_row(
                "SELECT count FROM rate_counters WHERE key = ?1",
                [&dk],
                |row| row.get(0),
            )?;
            let monthly: u64 = conn.query_row(
                "SELECT count FROM rate_counters WHERE key = ?1",
                [&mk],
                |row| row.get(0),
            )?;

            if daily >= daily_limit || monthly >= monthly_limit {
                return Ok(RateLimitResult {
                    ok: false,
                    daily,
                    monthly,
                });
            }

            // Increment
            conn.execute(
                "UPDATE rate_counters SET count = count + 1 WHERE key = ?1",
                [&dk],
            )?;
            conn.execute(
                "UPDATE rate_counters SET count = count + 1 WHERE key = ?1",
                [&mk],
            )?;

            Ok(RateLimitResult {
                ok: true,
                daily: daily + 1,
                monthly: monthly + 1,
            })
        })
        .await?
    }

    async fn get_stats(&self) -> Result<Stats> {
        let conn = self.conn.clone();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            let dk = today_key();
            let mk = month_key();

            let daily: u64 = conn
                .query_row(
                    "SELECT count FROM rate_counters WHERE key = ?1",
                    [&dk],
                    |row| row.get(0),
                )
                .unwrap_or(0);
            let monthly: u64 = conn
                .query_row(
                    "SELECT count FROM rate_counters WHERE key = ?1",
                    [&mk],
                    |row| row.get(0),
                )
                .unwrap_or(0);

            Ok(Stats { daily, monthly })
        })
        .await?
    }

    async fn cleanup_expired_pending(&self, max_age_secs: i64) -> Result<u64> {
        let conn = self.conn.clone();
        tokio::task::spawn_blocking(move || {
            let conn = conn.blocking_lock();
            let cutoff = now_epoch() - max_age_secs;
            let deleted = conn.execute(
                "DELETE FROM pending_callbacks WHERE created_at < ?1",
                [cutoff],
            )?;
            Ok(deleted as u64)
        })
        .await?
    }
}

// ── Tests ──

#[cfg(test)]
mod tests {
    use super::*;

    async fn test_store() -> SqliteStore {
        SqliteStore::new(":memory:").unwrap()
    }

    #[tokio::test]
    async fn token_round_trip() {
        let store = test_store().await;
        assert!(!store.token_exists("abc").await.unwrap());
        store.put_token("abc").await.unwrap();
        assert!(store.token_exists("abc").await.unwrap());
    }

    #[tokio::test]
    async fn user_mapping_crud() {
        let store = test_store().await;
        assert!(store.get_user_mapping("kakao1").await.unwrap().is_none());
        store.put_user_mapping("kakao1", "tok1").await.unwrap();
        assert_eq!(
            store.get_user_mapping("kakao1").await.unwrap().as_deref(),
            Some("tok1")
        );
        store.delete_user_mapping("kakao1").await.unwrap();
        assert!(store.get_user_mapping("kakao1").await.unwrap().is_none());
    }

    #[tokio::test]
    async fn killswitch_default_false() {
        let store = test_store().await;
        assert!(!store.get_killswitch().await.unwrap());
    }

    #[tokio::test]
    async fn killswitch_toggle() {
        let store = test_store().await;
        store.set_killswitch(true).await.unwrap();
        assert!(store.get_killswitch().await.unwrap());
        store.set_killswitch(false).await.unwrap();
        assert!(!store.get_killswitch().await.unwrap());
    }

    #[tokio::test]
    async fn pending_put_take_atomic() {
        let store = test_store().await;
        let ctx = PendingContext {
            callback_url: "https://cb.kakao.com/x".to_string(),
            user_id: "user1".to_string(),
            created_at: now_epoch(),
        };
        store.put_pending("act1", &ctx).await.unwrap();

        // First take succeeds
        let taken = store.take_pending("act1").await.unwrap();
        assert!(taken.is_some());
        assert_eq!(taken.unwrap().callback_url, "https://cb.kakao.com/x");

        // Second take returns None (atomic claim)
        assert!(store.take_pending("act1").await.unwrap().is_none());
    }

    #[tokio::test]
    async fn rate_limit_increments_and_caps() {
        let store = test_store().await;

        let r1 = store.check_rate_limit(3, 100).await.unwrap();
        assert!(r1.ok);
        assert_eq!(r1.daily, 1);

        let r2 = store.check_rate_limit(3, 100).await.unwrap();
        assert!(r2.ok);
        assert_eq!(r2.daily, 2);

        let r3 = store.check_rate_limit(3, 100).await.unwrap();
        assert!(r3.ok);
        assert_eq!(r3.daily, 3);

        // 4th request exceeds daily limit of 3
        let r4 = store.check_rate_limit(3, 100).await.unwrap();
        assert!(!r4.ok);
        assert_eq!(r4.daily, 3);
    }

    #[tokio::test]
    async fn stats_match_after_increments() {
        let store = test_store().await;
        store.check_rate_limit(100, 100).await.unwrap();
        store.check_rate_limit(100, 100).await.unwrap();

        let stats = store.get_stats().await.unwrap();
        assert_eq!(stats.daily, 2);
        assert_eq!(stats.monthly, 2);
    }

    #[tokio::test]
    async fn cleanup_expired_pending_removes_old() {
        let store = test_store().await;
        let old = PendingContext {
            callback_url: "https://cb.kakao.com/old".to_string(),
            user_id: "u1".to_string(),
            created_at: now_epoch() - 700, // 700 seconds ago
        };
        let fresh = PendingContext {
            callback_url: "https://cb.kakao.com/new".to_string(),
            user_id: "u2".to_string(),
            created_at: now_epoch(),
        };
        store.put_pending("old_act", &old).await.unwrap();
        store.put_pending("new_act", &fresh).await.unwrap();

        let deleted = store.cleanup_expired_pending(600).await.unwrap();
        assert_eq!(deleted, 1);

        // Old is gone, fresh remains
        assert!(store.take_pending("old_act").await.unwrap().is_none());
        assert!(store.take_pending("new_act").await.unwrap().is_some());
    }
}
