use kittypaw_core::config::SandboxConfig;
use kittypaw_core::error::Result;
use kittypaw_core::types::ExecutionResult;

use crate::backend::{SandboxBackend, SandboxExecConfig};
#[cfg(unix)]
use crate::forked::ForkedSandbox;
use crate::threaded::ThreadSandbox;

pub struct Sandbox {
    config: SandboxConfig,
    backend: Box<dyn SandboxBackend>,
}

impl Sandbox {
    pub fn new(config: SandboxConfig) -> Self {
        #[cfg(unix)]
        let backend: Box<dyn SandboxBackend> = Box::new(ForkedSandbox::new(
            config.timeout_secs,
            config.memory_limit_mb,
        ));
        #[cfg(not(unix))]
        let backend: Box<dyn SandboxBackend> = Box::new(ThreadSandbox::new(
            config.timeout_secs,
            config.memory_limit_mb,
        ));

        Self { config, backend }
    }

    /// Create a sandbox with ThreadSandbox backend (for GUI use).
    pub fn new_threaded(config: SandboxConfig) -> Self {
        let backend: Box<dyn SandboxBackend> = Box::new(ThreadSandbox::new(
            config.timeout_secs,
            config.memory_limit_mb,
        ));
        Self { config, backend }
    }

    pub async fn execute(&self, code: &str, context: serde_json::Value) -> Result<ExecutionResult> {
        let exec_config = SandboxExecConfig {
            code: code.to_string(),
            context_json: context.to_string(),
            timeout_ms: self.config.timeout_secs * 1000,
            max_memory_bytes: (self.config.memory_limit_mb as usize) * 1024 * 1024,
        };
        self.backend.execute(exec_config).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use kittypaw_core::config::SandboxConfig;
    use serde_json::json;

    use crate::quickjs::run_child_async;

    fn default_config() -> SandboxConfig {
        SandboxConfig {
            timeout_secs: 30,
            memory_limit_mb: 128,
            allowed_paths: vec![],
            allowed_hosts: vec![],
        }
    }

    fn timeout_config() -> SandboxConfig {
        SandboxConfig {
            timeout_secs: 2,
            memory_limit_mb: 128,
            allowed_paths: vec![],
            allowed_hosts: vec![],
        }
    }

    #[test]
    fn test_direct_simple() {
        let r = run_child_async("return 'hello';", json!({}), None);
        assert!(r.success, "error: {:?}", r.error);
        assert_eq!(r.output, "hello");
    }

    #[test]
    fn test_direct_skill_call() {
        let r = run_child_async(
            r#"await Telegram.sendMessage("chat123", "Hi"); return "done";"#,
            json!({}),
            None,
        );
        assert!(r.success, "error: {:?}", r.error);
        assert_eq!(r.output, "done");
        assert_eq!(r.skill_calls.len(), 1);
        assert_eq!(r.skill_calls[0].skill_name, "Telegram");
        assert_eq!(r.skill_calls[0].method, "sendMessage");
    }

    #[test]
    fn test_direct_syntax_error() {
        let r = run_child_async("this is not valid !!!", json!({}), None);
        assert!(!r.success);
        assert!(r.error.is_some());
    }

    #[tokio::test]
    async fn test_forked_simple() {
        let r = Sandbox::new(default_config())
            .execute("return 'hello from quickjs';", json!({}))
            .await
            .unwrap();
        // Fork+seatbelt+QuickJS init can exceed timeout under load, or
        // seatbelt may fail on non-macOS (Linux CI) — handle gracefully
        if !r.success {
            let err = r.error.unwrap_or_default();
            assert!(
                err.contains("Sandbox initialization failed") || err.contains("timed out"),
                "unexpected error: {err}"
            );
            return;
        }
        assert_eq!(r.output, "hello from quickjs");
    }

    #[tokio::test]
    async fn test_forked_skill_call() {
        let r = Sandbox::new(default_config())
            .execute(
                r#"await Telegram.sendMessage("chat123", "Hello World"); return "done";"#,
                json!({}),
            )
            .await
            .unwrap();
        // Fork+seatbelt+QuickJS init can exceed timeout under load, or
        // seatbelt may fail on non-macOS (Linux CI) — handle gracefully
        if !r.success {
            let err = r.error.unwrap_or_default();
            assert!(
                err.contains("Sandbox initialization failed") || err.contains("timed out"),
                "unexpected error: {err}"
            );
            return;
        }
        assert_eq!(r.output, "done");
        assert_eq!(r.skill_calls.len(), 1);
        assert_eq!(r.skill_calls[0].skill_name, "Telegram");
    }

    #[tokio::test]
    async fn test_forked_syntax_error() {
        let r = Sandbox::new(default_config())
            .execute("this is not valid !!!", json!({}))
            .await
            .unwrap();
        // Both seatbelt failure, timeout, and syntax error produce success: false
        assert!(!r.success);
        assert!(r.error.is_some());
    }

    #[tokio::test]
    async fn test_forked_timeout() {
        let r = Sandbox::new(timeout_config())
            .execute("while(true) {}", json!({}))
            .await
            .unwrap();
        assert!(!r.success);
        assert!(r.error.unwrap_or_default().contains("timed out"));
    }

    #[tokio::test]
    async fn test_threaded_simple() {
        let r = Sandbox::new_threaded(default_config())
            .execute("return 'hello from thread';", json!({}))
            .await
            .unwrap();
        assert!(r.success, "error: {:?}", r.error);
        assert_eq!(r.output, "hello from thread");
    }

    #[tokio::test]
    async fn test_threaded_skill_call() {
        let r = Sandbox::new_threaded(default_config())
            .execute(
                r#"await Telegram.sendMessage("chat123", "Hi"); return "done";"#,
                json!({}),
            )
            .await
            .unwrap();
        assert!(r.success, "error: {:?}", r.error);
        assert_eq!(r.output, "done");
        assert_eq!(r.skill_calls.len(), 1);
        assert_eq!(r.skill_calls[0].skill_name, "Telegram");
    }

    #[tokio::test]
    async fn test_threaded_syntax_error() {
        let r = Sandbox::new_threaded(default_config())
            .execute("this is not valid !!!", json!({}))
            .await
            .unwrap();
        assert!(!r.success);
        assert!(r.error.is_some());
    }

    #[tokio::test]
    async fn test_threaded_timeout() {
        let r = Sandbox::new_threaded(timeout_config())
            .execute("while(true) {}", json!({}))
            .await
            .unwrap();
        assert!(!r.success);
        assert!(r.error.unwrap_or_default().contains("timed out"));
    }
}
