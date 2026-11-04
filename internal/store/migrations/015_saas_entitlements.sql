CREATE TABLE saas_accounts (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id),
    plan_code text NOT NULL CHECK (plan_code IN ('free','pilot')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    community_limit integer NOT NULL CHECK (community_limit BETWEEN 1 AND 1000),
    managed_billing_allowed boolean NOT NULL DEFAULT false,
    granted_by bigint,
    granted_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (plan_code='pilot' OR (community_limit=1 AND managed_billing_allowed=false))
);

INSERT INTO saas_accounts(workspace_id,plan_code,community_limit,managed_billing_allowed,granted_reason)
SELECT w.id,
       CASE WHEN count(c.id)>1 OR bool_or(c.billing_mode='managed_bot') THEN 'pilot' ELSE 'free' END,
       CASE WHEN count(c.id)>1 OR bool_or(c.billing_mode='managed_bot')
            THEN greatest(5,count(c.id))::integer ELSE 1 END,
       coalesce(bool_or(c.billing_mode='managed_bot'),false),
       'Existing workspace migration; operator review required'
FROM workspaces w LEFT JOIN communities c ON c.workspace_id=w.id AND c.status<>'archived'
GROUP BY w.id
ON CONFLICT (workspace_id) DO NOTHING;

CREATE FUNCTION enforce_community_saas_limit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE account saas_accounts%ROWTYPE;
DECLARE used_count integer;
BEGIN
    SELECT * INTO account FROM saas_accounts WHERE workspace_id=NEW.workspace_id FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'SaaS account missing for workspace'; END IF;
    IF account.status<>'active' THEN RAISE EXCEPTION 'SaaS account is suspended'; END IF;
    IF NEW.billing_mode='managed_bot' AND NOT account.managed_billing_allowed THEN
        RAISE EXCEPTION 'managed billing is not enabled for this SaaS account';
    END IF;
    SELECT count(*) INTO used_count FROM communities
      WHERE workspace_id=NEW.workspace_id AND status<>'archived';
    IF used_count>=account.community_limit THEN
        RAISE EXCEPTION 'community limit reached for this SaaS account';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER communities_saas_limit BEFORE INSERT ON communities
    FOR EACH ROW EXECUTE FUNCTION enforce_community_saas_limit();

CREATE FUNCTION create_workspace_saas_account() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO saas_accounts(workspace_id,plan_code,community_limit)
      VALUES(NEW.id,'free',1);
    RETURN NEW;
END;
$$;
CREATE TRIGGER workspaces_saas_account AFTER INSERT ON workspaces
    FOR EACH ROW EXECUTE FUNCTION create_workspace_saas_account();

CREATE TABLE saas_financial_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    provider text NOT NULL,
    provider_event_id text NOT NULL,
    amount_minor bigint NOT NULL,
    currency text NOT NULL CHECK (length(currency)=3),
    kind text NOT NULL CHECK (kind IN ('charge','refund')),
    occurred_at timestamptz NOT NULL,
    raw jsonb NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE(provider,provider_event_id)
);
CREATE INDEX saas_financial_workspace_idx ON saas_financial_events(workspace_id,occurred_at DESC);
