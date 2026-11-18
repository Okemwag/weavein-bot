ALTER TABLE telegram_updates DROP CONSTRAINT telegram_updates_state_check;
ALTER TABLE telegram_updates ADD CONSTRAINT telegram_updates_state_check
    CHECK (state IN ('pending','awaiting_answer','answering','answer_unknown','processed','quarantined','failed'));
CREATE INDEX telegram_updates_answering_idx ON telegram_updates(received_at) WHERE state='answering';
