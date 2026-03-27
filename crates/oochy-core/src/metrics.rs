use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

#[derive(Debug, Clone)]
pub struct Metrics {
    inner: Arc<MetricsInner>,
}

#[derive(Debug)]
struct MetricsInner {
    pub request_count: AtomicU64,
    pub error_count: AtomicU64,
    pub llm_calls: AtomicU64,
    pub sandbox_executions: AtomicU64,
    pub skill_calls_total: AtomicU64,
    pub started_at: std::time::Instant,
}

impl Metrics {
    pub fn new() -> Self {
        Self {
            inner: Arc::new(MetricsInner {
                request_count: AtomicU64::new(0),
                error_count: AtomicU64::new(0),
                llm_calls: AtomicU64::new(0),
                sandbox_executions: AtomicU64::new(0),
                skill_calls_total: AtomicU64::new(0),
                started_at: std::time::Instant::now(),
            }),
        }
    }

    pub fn inc_requests(&self) {
        self.inner.request_count.fetch_add(1, Ordering::Relaxed);
    }

    pub fn inc_errors(&self) {
        self.inner.error_count.fetch_add(1, Ordering::Relaxed);
    }

    pub fn inc_llm_calls(&self) {
        self.inner.llm_calls.fetch_add(1, Ordering::Relaxed);
    }

    pub fn inc_sandbox_executions(&self) {
        self.inner.sandbox_executions.fetch_add(1, Ordering::Relaxed);
    }

    pub fn add_skill_calls(&self, n: u64) {
        self.inner.skill_calls_total.fetch_add(n, Ordering::Relaxed);
    }

    pub fn uptime_secs(&self) -> u64 {
        self.inner.started_at.elapsed().as_secs()
    }

    pub fn snapshot(&self) -> MetricsSnapshot {
        MetricsSnapshot {
            request_count: self.inner.request_count.load(Ordering::Relaxed),
            error_count: self.inner.error_count.load(Ordering::Relaxed),
            llm_calls: self.inner.llm_calls.load(Ordering::Relaxed),
            sandbox_executions: self.inner.sandbox_executions.load(Ordering::Relaxed),
            skill_calls_total: self.inner.skill_calls_total.load(Ordering::Relaxed),
            uptime_secs: self.uptime_secs(),
        }
    }
}

impl Default for Metrics {
    fn default() -> Self {
        Self::new()
    }
}

#[derive(Debug, serde::Serialize)]
pub struct MetricsSnapshot {
    pub request_count: u64,
    pub error_count: u64,
    pub llm_calls: u64,
    pub sandbox_executions: u64,
    pub skill_calls_total: u64,
    pub uptime_secs: u64,
}
