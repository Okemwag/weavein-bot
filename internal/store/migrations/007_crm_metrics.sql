CREATE TABLE member_notes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL REFERENCES communities(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    author_telegram_user_id bigint NOT NULL CHECK (author_telegram_user_id > 0),
    body text NOT NULL CHECK (length(trim(body)) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX member_notes_subject_idx ON member_notes(community_id,telegram_user_id,created_at DESC);
CREATE INDEX payments_paid_at_idx ON payments(paid_at DESC);
CREATE INDEX subscription_events_kind_at_idx ON subscription_events(kind,occurred_at DESC);
CREATE INDEX subscriptions_first_paid_idx ON subscriptions(community_id,first_paid_at DESC);
