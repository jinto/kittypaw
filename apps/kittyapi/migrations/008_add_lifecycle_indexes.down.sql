-- 008 down policy: 인덱스만 제거. 데이터 손실 위험 0.
DROP INDEX IF EXISTS idx_refresh_tokens_expires;
DROP INDEX IF EXISTS idx_devices_last_used;
