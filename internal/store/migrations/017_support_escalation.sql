ALTER TABLE support_tickets ADD COLUMN assigned_operator_id bigint;
ALTER TABLE support_tickets ADD COLUMN escalated_at timestamptz;
ALTER TABLE support_tickets ADD COLUMN escalation_reason text;
CREATE INDEX support_tickets_escalation_idx ON support_tickets(created_at)
    WHERE status<>'resolved' AND escalated_at IS NULL;
