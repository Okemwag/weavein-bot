CREATE TABLE access_overrides (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL REFERENCES communities(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    valid_until timestamptz NOT NULL,
    reason text NOT NULL CHECK (length(trim(reason)) BETWEEN 10 AND 500),
    active boolean NOT NULL DEFAULT true,
    created_by bigint NOT NULL CHECK (created_by > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
CREATE INDEX access_overrides_current_idx ON access_overrides(community_id,telegram_user_id,valid_until)
    WHERE active=true;
CREATE INDEX access_overrides_due_idx ON access_overrides(valid_until) WHERE active=true;
