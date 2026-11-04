ALTER TABLE access_jobs DROP CONSTRAINT access_jobs_action_check;
ALTER TABLE access_jobs ADD CONSTRAINT access_jobs_action_check
    CHECK (action IN ('approve','remove','decline'));
ALTER TABLE access_jobs DROP CONSTRAINT access_jobs_status_check;
ALTER TABLE access_jobs ADD CONSTRAINT access_jobs_status_check
    CHECK (status IN ('pending','awaiting_confirmation','done','failed'));
