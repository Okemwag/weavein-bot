-- MemberOS Pro: a creator workspace subscription paid in Telegram Stars to the
-- MemberOS bot. Telegram requires Stars for digital services sold in bots.
-- community_limit stays the effective allowance so every existing check keeps
-- working: a Pro payment raises it and a lapse lowers it. Lowering it never
-- archives or deletes a community; it only stops new ones from being added.

ALTER TABLE saas_accounts DROP CONSTRAINT saas_accounts_plan_code_check;
ALTER TABLE saas_accounts ADD CONSTRAINT saas_accounts_plan_code_check
    CHECK (plan_code IN ('free','pilot','pro'));
ALTER TABLE saas_accounts DROP CONSTRAINT saas_accounts_check;
ALTER TABLE saas_accounts ADD CONSTRAINT saas_accounts_plan_limits CHECK (
    plan_code='pilot'
    OR (managed_billing_allowed=false AND (plan_code='pro' OR community_limit=1)));
ALTER TABLE saas_accounts ADD COLUMN pro_paid_through timestamptz;
ALTER TABLE saas_accounts ADD COLUMN lapsed_at timestamptz;

CREATE TABLE saas_checkouts (
    invoice_payload text PRIMARY KEY CHECK (invoice_payload LIKE 'saas:%'),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    billing_bot_id bigint NOT NULL CHECK (billing_bot_id > 0),
    price_stars integer NOT NULL CHECK (price_stars BETWEEN 1 AND 10000),
    community_limit integer NOT NULL CHECK (community_limit BETWEEN 2 AND 1000),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid','failed')),
    invoice_url text,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saas_checkouts_workspace_idx ON saas_checkouts(workspace_id, created_at DESC);

CREATE TABLE saas_subscriptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    invoice_payload text NOT NULL UNIQUE REFERENCES saas_checkouts(invoice_payload),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    billing_bot_id bigint NOT NULL CHECK (billing_bot_id > 0),
    first_charge_id text NOT NULL,
    community_limit integer NOT NULL CHECK (community_limit BETWEEN 2 AND 1000),
    state text NOT NULL CHECK (state IN ('active','cancel_scheduled','payment_failed','expired','refunded')),
    paid_through timestamptz NOT NULL,
    last_billing_seq bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saas_subscriptions_workspace_idx ON saas_subscriptions(workspace_id, paid_through DESC);

-- One row per Stars charge, so renewals and refunds can be matched to a payer.
CREATE TABLE saas_payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    saas_subscription_id uuid NOT NULL REFERENCES saas_subscriptions(id),
    billing_bot_id bigint NOT NULL,
    telegram_charge_id text NOT NULL,
    price_stars integer NOT NULL CHECK (price_stars > 0),
    paid_at timestamptz NOT NULL,
    paid_through timestamptz NOT NULL,
    refunded_at timestamptz,
    -- An operator refund is claimed once. 'needs_review' means Telegram's answer
    -- was uncertain, so nobody may resubmit before checking the transaction.
    refund_status text CHECK (refund_status IN ('submitted','needs_review')),
    refund_requested_by bigint,
    refund_reason text,
    UNIQUE (billing_bot_id, telegram_charge_id)
);
