ALTER TABLE communities ADD COLUMN billing_mode text NOT NULL DEFAULT 'native_channel'
    CHECK (billing_mode IN ('native_channel', 'managed_bot'));
ALTER TABLE communities ALTER COLUMN native_invite_link DROP NOT NULL;
ALTER TABLE communities ADD COLUMN managed_join_link text;
ALTER TABLE communities ADD COLUMN billing_bot_id bigint;
ALTER TABLE communities ADD CONSTRAINT community_billing_mode_fields CHECK (
    (billing_mode = 'native_channel' AND native_invite_link IS NOT NULL AND billing_bot_id IS NULL)
    OR (billing_mode = 'managed_bot' AND managed_join_link IS NOT NULL AND billing_bot_id IS NOT NULL)
);

ALTER TABLE telegram_updates DROP CONSTRAINT telegram_updates_state_check;
ALTER TABLE telegram_updates ADD CONSTRAINT telegram_updates_state_check
    CHECK (state IN ('pending','awaiting_answer','processed','quarantined'));

CREATE TABLE checkout_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL REFERENCES communities(id),
    creator_id uuid NOT NULL REFERENCES creators(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    billing_bot_id bigint NOT NULL,
    invoice_payload text NOT NULL UNIQUE,
    price_stars integer NOT NULL CHECK (price_stars BETWEEN 1 AND 10000),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid','failed','expired')),
    invoice_url text,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > created_at)
);
CREATE INDEX checkout_member_idx ON checkout_attempts(community_id, telegram_user_id, created_at DESC);

CREATE TABLE subscriptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL REFERENCES communities(id),
    creator_id uuid NOT NULL REFERENCES creators(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    billing_bot_id bigint NOT NULL,
    invoice_payload text NOT NULL UNIQUE REFERENCES checkout_attempts(invoice_payload),
    state text NOT NULL CHECK (state IN ('active','cancel_scheduled','payment_failed','expired','refunded')),
    auto_renew_state text NOT NULL DEFAULT 'unknown' CHECK (auto_renew_state IN ('unknown','on','off','failed')),
    paid_through timestamptz NOT NULL,
    first_paid_at timestamptz NOT NULL,
    last_payment_at timestamptz NOT NULL,
    last_failed_at timestamptz,
    canceled_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE subscriptions ADD COLUMN last_subscription_update_id bigint NOT NULL DEFAULT 0;
CREATE INDEX subscriptions_due_idx ON subscriptions(paid_through) WHERE state <> 'expired';
CREATE INDEX subscriptions_member_idx ON subscriptions(community_id,telegram_user_id);

CREATE TABLE payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES subscriptions(id),
    billing_bot_id bigint NOT NULL,
    telegram_charge_id text NOT NULL,
    invoice_payload text NOT NULL,
    price_stars integer NOT NULL CHECK (price_stars > 0),
    provider_expiration timestamptz NOT NULL,
    paid_at timestamptz NOT NULL,
    is_first_recurring boolean NOT NULL,
    refunded_at timestamptz,
    UNIQUE (billing_bot_id, telegram_charge_id)
);
CREATE INDEX payments_subscription_idx ON payments(subscription_id,provider_expiration DESC);

CREATE TABLE entitlements (
    subscription_id uuid PRIMARY KEY REFERENCES subscriptions(id),
    community_id uuid NOT NULL REFERENCES communities(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    valid_until timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('active','expired')),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX entitlements_access_idx ON entitlements(community_id,telegram_user_id,valid_until) WHERE status='active';

CREATE TABLE join_requests (
    community_id uuid NOT NULL REFERENCES communities(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    requested_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('pending','approved','ineligible')),
    PRIMARY KEY (community_id,telegram_user_id)
);

CREATE TABLE access_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL REFERENCES communities(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    action text NOT NULL CHECK (action IN ('approve','remove')),
    dedupe_key text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','failed')),
    run_after timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0,
    last_error text,
    finished_at timestamptz
);
CREATE INDEX access_jobs_due_idx ON access_jobs(run_after) WHERE status='pending';
