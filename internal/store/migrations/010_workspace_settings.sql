ALTER TABLE workspaces ADD COLUMN timezone text NOT NULL DEFAULT 'UTC';
ALTER TABLE workspaces ADD COLUMN language text NOT NULL DEFAULT 'en';
ALTER TABLE workspaces ADD COLUMN notifications_enabled boolean NOT NULL DEFAULT true;
ALTER TABLE workspaces ADD COLUMN deletion_requested_at timestamptz;
ALTER TABLE workspaces ADD COLUMN deletion_requested_by bigint;
