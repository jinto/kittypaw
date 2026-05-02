-- 006 down policy: silent CASCADE. devices 테이블 자체를 제거하므로
-- 운영자가 down을 실행한 시점에 device 기능 전체 롤백 의도가 명확.
-- ON DELETE CASCADE로 refresh_tokens.device_id 행도 같이 정리됨.
-- (007의 ALTER TABLE DROP COLUMN과 비대칭 — 007은 user/device 같은
-- 테이블 공유라 abort guard 필수, 006은 device-only 테이블이라 단순.)
DROP TABLE IF EXISTS devices;
