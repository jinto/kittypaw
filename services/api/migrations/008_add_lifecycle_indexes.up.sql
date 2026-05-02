-- Plan 24 T1 — Credential Lifecycle Janitor가 매일 KST 04:00에 사용하는
-- scan 인덱스. 이 인덱스 없으면 ReapIdle / DeleteExpiredOlderThan이
-- full table scan으로 전락 → autovacuum 압력 + lock 시간 증가.
--
-- 두 인덱스 모두 partial (revoked_at IS NULL) — janitor는 active row만
-- 보면 충분하고, revoked row는 별도 (revoked_at) 인덱스 없이 90일 retention
-- DELETE에서 한 번만 scan되면 됨 (혜택 < 인덱스 유지비).
CREATE INDEX IF NOT EXISTS idx_devices_last_used
  ON devices(last_used_at) WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires
  ON refresh_tokens(expires_at) WHERE revoked_at IS NULL;
