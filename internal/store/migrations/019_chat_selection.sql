CREATE TABLE chat_selection_requests (
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    request_id integer NOT NULL CHECK (request_id > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (telegram_user_id, request_id)
);

CREATE TABLE chat_selections (
    telegram_user_id bigint PRIMARY KEY CHECK (telegram_user_id > 0),
    telegram_chat_id bigint NOT NULL,
    title text NOT NULL DEFAULT '',
    selected_at timestamptz NOT NULL DEFAULT now()
);
