-- Telegram picks a random update_id after a week without updates, so update_id
-- cannot order events. seq records the order updates reached this inbox.
ALTER TABLE telegram_updates ADD COLUMN seq bigint;
CREATE SEQUENCE telegram_updates_seq_seq OWNED BY telegram_updates.seq;
WITH ordered AS (
    SELECT bot_id,update_id,row_number() OVER (ORDER BY received_at,update_id) AS position FROM telegram_updates
) UPDATE telegram_updates t SET seq=o.position FROM ordered o
    WHERE t.bot_id=o.bot_id AND t.update_id=o.update_id;
SELECT setval('telegram_updates_seq_seq',coalesce((SELECT max(seq) FROM telegram_updates),0)+1,false);
ALTER TABLE telegram_updates ALTER COLUMN seq SET DEFAULT nextval('telegram_updates_seq_seq');
ALTER TABLE telegram_updates ALTER COLUMN seq SET NOT NULL;

ALTER TABLE subscriptions ADD COLUMN last_billing_seq bigint NOT NULL DEFAULT 0;
UPDATE subscriptions s SET last_billing_seq=t.seq FROM telegram_updates t
    WHERE t.bot_id=s.billing_bot_id AND t.update_id=s.last_billing_update_id;
ALTER TABLE subscriptions DROP COLUMN last_billing_update_id;

-- ignored: expected traffic needing no action. acknowledged: reviewed by an operator.
ALTER TABLE telegram_updates DROP CONSTRAINT telegram_updates_state_check;
ALTER TABLE telegram_updates ADD CONSTRAINT telegram_updates_state_check
    CHECK (state IN ('pending','awaiting_answer','answering','answer_unknown','processed','quarantined','failed','ignored','acknowledged'));
UPDATE telegram_updates SET state='ignored' WHERE state='quarantined' AND update_type='unsupported';
