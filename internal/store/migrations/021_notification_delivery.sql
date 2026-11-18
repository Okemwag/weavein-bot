ALTER TABLE notifications DROP CONSTRAINT notifications_status_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_status_check
    CHECK (status IN ('pending','sending','sent','failed','uncertain'));
ALTER TABLE notifications ADD COLUMN last_attempt_at timestamptz;
ALTER TABLE notifications ADD COLUMN telegram_message_id bigint;
CREATE INDEX notifications_sending_idx ON notifications(last_attempt_at) WHERE status='sending';
