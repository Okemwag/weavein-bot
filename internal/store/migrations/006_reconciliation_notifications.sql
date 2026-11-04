ALTER TABLE communities ADD COLUMN connection_status text NOT NULL DEFAULT 'unknown'
    CHECK (connection_status IN ('unknown','healthy','degraded','disconnected'));
ALTER TABLE communities ADD COLUMN last_permission_check_at timestamptz;
ALTER TABLE communities ADD COLUMN last_permission_error text;

CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id bigint NOT NULL,
    telegram_user_id bigint NOT NULL,
    dedupe_key text NOT NULL UNIQUE,
    message text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sent','failed')),
    attempts integer NOT NULL DEFAULT 0,
    run_after timestamptz NOT NULL DEFAULT now(),
    last_error text,
    sent_at timestamptz
);
CREATE INDEX notifications_due_idx ON notifications(run_after) WHERE status='pending';

ALTER TABLE observed_star_transactions ADD COLUMN direction text;
UPDATE observed_star_transactions SET direction=CASE WHEN raw ? 'receiver' THEN 'outgoing' ELSE 'incoming' END;
ALTER TABLE observed_star_transactions ALTER COLUMN direction SET NOT NULL;
ALTER TABLE observed_star_transactions ADD CONSTRAINT observed_star_transactions_direction_check
    CHECK (direction IN ('incoming','outgoing'));
ALTER TABLE observed_star_transactions DROP CONSTRAINT observed_star_transactions_pkey;
ALTER TABLE observed_star_transactions ADD PRIMARY KEY (bot_id,transaction_id,direction);
