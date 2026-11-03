ALTER TABLE telegram_updates ADD COLUMN run_after timestamptz NOT NULL DEFAULT now();
ALTER TABLE telegram_updates DROP CONSTRAINT telegram_updates_state_check;
ALTER TABLE telegram_updates ADD CONSTRAINT telegram_updates_state_check
    CHECK (state IN ('pending','awaiting_answer','processed','quarantined','failed'));
CREATE INDEX telegram_updates_due_idx ON telegram_updates(run_after) WHERE state='pending';

CREATE TABLE subscription_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES subscriptions(id),
    event_key text NOT NULL UNIQUE,
    kind text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    details jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX subscription_events_history_idx ON subscription_events(subscription_id,occurred_at DESC);
CREATE FUNCTION reject_subscription_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'subscription events are immutable'; END $$;
CREATE TRIGGER subscription_events_immutable BEFORE UPDATE OR DELETE ON subscription_events
    FOR EACH ROW EXECUTE FUNCTION reject_subscription_event_mutation();

CREATE TABLE support_tickets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL REFERENCES communities(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    kind text NOT NULL CHECK (kind IN ('access','payment','refund','other')),
    description text NOT NULL CHECK (length(description) BETWEEN 10 AND 2000),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','in_progress','resolved')),
    resolution text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX support_tickets_community_idx ON support_tickets(community_id,status,created_at DESC);

CREATE TABLE refund_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id uuid NOT NULL REFERENCES payments(id),
    ticket_id uuid REFERENCES support_tickets(id),
    requested_by bigint NOT NULL,
    status text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','processing','submitted','confirmed','rejected','needs_review')),
    reason text NOT NULL,
    decided_by bigint,
    decision_note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX refund_requests_open_idx ON refund_requests(payment_id)
    WHERE status IN ('requested','processing','submitted','needs_review');

CREATE TABLE star_reconciliation_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('running','complete','failed')),
    balance_stars bigint,
    transaction_count integer NOT NULL DEFAULT 0,
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE TABLE observed_star_transactions (
    bot_id bigint NOT NULL,
    transaction_id text NOT NULL,
    amount_stars bigint NOT NULL,
    occurred_at timestamptz NOT NULL,
    raw jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (bot_id,transaction_id)
);
