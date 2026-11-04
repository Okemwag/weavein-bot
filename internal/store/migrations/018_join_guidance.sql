ALTER TABLE join_requests ADD COLUMN user_chat_id bigint;
ALTER TABLE join_requests ADD COLUMN guidance_attempted_at timestamptz;
