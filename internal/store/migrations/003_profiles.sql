CREATE TABLE telegram_profiles (
    telegram_user_id bigint PRIMARY KEY CHECK (telegram_user_id > 0),
    first_name text NOT NULL DEFAULT '',
    username text NOT NULL DEFAULT '',
    verified_at timestamptz NOT NULL DEFAULT now()
);
