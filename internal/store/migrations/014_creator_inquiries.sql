CREATE TABLE creator_inquiries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(trim(name)) BETWEEN 2 AND 120),
    email text NOT NULL CHECK (length(email) BETWEEN 3 AND 254),
    community_link text NOT NULL DEFAULT '',
    message text NOT NULL CHECK (length(trim(message)) BETWEEN 10 AND 2000),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
    resolution text,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);
CREATE INDEX creator_inquiries_open_idx ON creator_inquiries(created_at) WHERE status='open';
