CREATE TABLE bot_private_contacts (
    bot_id bigint NOT NULL,
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    first_contact_at timestamptz NOT NULL DEFAULT now(),
    last_contact_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (bot_id,telegram_user_id)
);
CREATE TABLE subscriber_notification_preferences (
    telegram_user_id bigint PRIMARY KEY CHECK (telegram_user_id > 0),
    reminders_enabled boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now()
);
