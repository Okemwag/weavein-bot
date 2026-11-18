ALTER TABLE subscriptions ADD COLUMN last_billing_update_id bigint NOT NULL DEFAULT 0;
UPDATE subscriptions SET last_billing_update_id=last_subscription_update_id;
