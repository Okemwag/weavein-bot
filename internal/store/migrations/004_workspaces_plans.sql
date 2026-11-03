CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id uuid NOT NULL UNIQUE REFERENCES creators(id),
    name text NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, creator_id)
);

CREATE TABLE workspace_members (
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    role text NOT NULL CHECK (role IN ('owner','admin')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, telegram_user_id)
);

INSERT INTO workspaces(creator_id,name) SELECT id,display_name FROM creators;
INSERT INTO workspace_members(workspace_id,telegram_user_id,role)
SELECT w.id,cu.telegram_user_id,cu.role FROM creator_users cu JOIN workspaces w ON w.creator_id=cu.creator_id;

ALTER TABLE communities ADD COLUMN workspace_id uuid;
UPDATE communities c SET workspace_id=w.id FROM workspaces w WHERE w.creator_id=c.creator_id;
ALTER TABLE communities ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE communities ADD CONSTRAINT communities_workspace_creator_fk
    FOREIGN KEY (workspace_id,creator_id) REFERENCES workspaces(id,creator_id);
ALTER TABLE communities ADD CONSTRAINT communities_id_workspace_unique UNIQUE (id,workspace_id);
CREATE INDEX communities_workspace_idx ON communities(workspace_id);

CREATE TABLE plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    community_id uuid NOT NULL,
    name text NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (community_id,name),
    UNIQUE (id,community_id),
    FOREIGN KEY (community_id,workspace_id) REFERENCES communities(id,workspace_id)
);
CREATE INDEX plans_community_idx ON plans(community_id,status);

CREATE TABLE plan_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id uuid NOT NULL REFERENCES plans(id),
    version integer NOT NULL CHECK (version > 0),
    price_stars integer NOT NULL CHECK (price_stars BETWEEN 1 AND 10000),
    period_seconds integer NOT NULL DEFAULT 2592000 CHECK (period_seconds=2592000),
    description text NOT NULL DEFAULT '',
    benefits text NOT NULL DEFAULT '',
    terms text NOT NULL CHECK (length(trim(terms)) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (plan_id,version)
);

INSERT INTO plans(workspace_id,community_id,name)
SELECT workspace_id,id,'Membership' FROM communities;
INSERT INTO plan_versions(plan_id,version,price_stars,description,terms)
SELECT p.id,1,c.price_stars,c.description,
    CASE WHEN c.billing_mode='managed_bot'
        THEN 'Recurring 30-day Telegram Stars subscription. Cancellation stops future renewals; paid access lasts until the paid-through date.'
        ELSE 'Telegram manages this 30-day channel subscription, billing and access.' END
FROM plans p JOIN communities c ON c.id=p.community_id;

ALTER TABLE checkout_attempts ADD COLUMN plan_version_id uuid REFERENCES plan_versions(id);
ALTER TABLE subscriptions ADD COLUMN plan_version_id uuid REFERENCES plan_versions(id);
CREATE INDEX checkout_plan_version_idx ON checkout_attempts(plan_version_id);

CREATE FUNCTION prevent_plan_version_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'plan versions are immutable';
END;
$$;
CREATE TRIGGER plan_versions_immutable BEFORE UPDATE OR DELETE ON plan_versions
    FOR EACH ROW EXECUTE FUNCTION prevent_plan_version_change();
