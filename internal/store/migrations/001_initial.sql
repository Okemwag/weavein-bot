CREATE TABLE creators (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name text NOT NULL,
    support_contact text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE creator_users (
    creator_id uuid NOT NULL REFERENCES creators(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    role text NOT NULL CHECK (role IN ('owner', 'admin')),
    verified_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (creator_id, telegram_user_id)
);

CREATE TABLE communities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id uuid NOT NULL REFERENCES creators(id),
    telegram_chat_id bigint NOT NULL UNIQUE,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    native_invite_link text NOT NULL,
    price_stars integer NOT NULL CHECK (price_stars BETWEEN 1 AND 10000),
    period_seconds integer NOT NULL DEFAULT 2592000 CHECK (period_seconds = 2592000),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    bot_admin_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX communities_creator_idx ON communities(creator_id);

CREATE TABLE members (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id uuid NOT NULL REFERENCES creators(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    first_name text NOT NULL DEFAULT '',
    username text NOT NULL DEFAULT '',
    verified_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (creator_id, telegram_user_id)
);

CREATE TABLE access_observations (
    community_id uuid NOT NULL REFERENCES communities(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    member_status text NOT NULL,
    subscription_until timestamptz,
    observed_at timestamptz NOT NULL,
    source text NOT NULL CHECK (source IN ('chat_member', 'getChatMember')),
    PRIMARY KEY (community_id, telegram_user_id)
);

CREATE TABLE telegram_updates (
    bot_id bigint NOT NULL,
    update_id bigint NOT NULL,
    update_type text NOT NULL,
    payload jsonb NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'processed', 'quarantined')),
    attempts integer NOT NULL DEFAULT 0,
    last_error text,
    processed_at timestamptz,
    PRIMARY KEY (bot_id, update_id)
);
CREATE INDEX telegram_updates_pending_idx ON telegram_updates(received_at) WHERE state = 'pending';

CREATE TABLE audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id uuid REFERENCES creators(id),
    subject_telegram_user_id bigint,
    action text NOT NULL,
    update_id bigint,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    happened_at timestamptz NOT NULL DEFAULT now()
);
