-- running: claimed by a worker that is calling Telegram outside any transaction.
-- cancel_renewal: stops Telegram charging a refunded or duplicate subscription.
ALTER TABLE access_jobs DROP CONSTRAINT IF EXISTS access_jobs_action_check;
ALTER TABLE access_jobs ADD CONSTRAINT access_jobs_action_check
    CHECK (action IN ('approve','remove','decline','cancel_renewal'));
ALTER TABLE access_jobs DROP CONSTRAINT IF EXISTS access_jobs_status_check;
ALTER TABLE access_jobs ADD CONSTRAINT access_jobs_status_check
    CHECK (status IN ('pending','running','awaiting_confirmation','done','failed','acknowledged'));
ALTER TABLE access_jobs ADD COLUMN claimed_at timestamptz;
ALTER TABLE access_jobs ADD COLUMN subscription_id uuid REFERENCES subscriptions(id);
CREATE INDEX access_jobs_running_idx ON access_jobs(community_id,telegram_user_id) WHERE status='running';

-- undeliverable: Telegram refused the recipient, such as a user who blocked the bot.
ALTER TABLE notifications DROP CONSTRAINT notifications_status_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_status_check
    CHECK (status IN ('pending','sending','sent','failed','uncertain','undeliverable','acknowledged'));
UPDATE notifications SET status='undeliverable' WHERE status='failed' AND last_error='Telegram rejected message delivery';

-- Disconnect commits first, then revokes the link, so a failed revoke can be retried.
ALTER TABLE communities ADD COLUMN join_link_revoked_at timestamptz;
UPDATE communities SET join_link_revoked_at=now() WHERE status='archived' AND billing_mode='managed_bot';

-- workspace_members is the only tenancy model read by the application.
INSERT INTO workspace_members(workspace_id,telegram_user_id,role)
SELECT w.id,cu.telegram_user_id,cu.role FROM creator_users cu JOIN workspaces w ON w.creator_id=cu.creator_id
ON CONFLICT (workspace_id,telegram_user_id) DO NOTHING;
