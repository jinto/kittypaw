-- ABORT POLICY: refresh_tokens에 device_id IS NOT NULL row가 존재하면
-- DROP COLUMN은 device refresh 데이터를 silent하게 삭제하면서 user
-- refresh row는 외관상 정상으로 남아 운영 데이터 검증 회피. 그래서
-- 명시적 abort. dev/CI 환경에서 down 진행하려면 사전에 다음을 실행:
--   DELETE FROM refresh_tokens WHERE device_id IS NOT NULL;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM refresh_tokens WHERE device_id IS NOT NULL) THEN
        RAISE EXCEPTION 'refresh_tokens contains device_id rows; explicit cascade required';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_refresh_tokens_device_id;
ALTER TABLE refresh_tokens DROP COLUMN device_id;
