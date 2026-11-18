-- The single definition of "may this user be in this community right now?".
-- A renewal that is still on gets a 24 hour grace window, so a late Telegram
-- renewal or a webhook outage cannot remove a member who is about to be charged.
-- Cancelled or failed renewals get no grace.
CREATE FUNCTION entitlement_is_current(status text, valid_until timestamptz, auto_renew text, sub_state text)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT status='active' AND (valid_until>now()
        OR (valid_until>now()-interval '24 hours' AND auto_renew='on' AND sub_state='active'))
$$;

CREATE FUNCTION member_has_access(p_community uuid, p_user bigint)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM entitlements e JOIN subscriptions s ON s.id=e.subscription_id
            WHERE e.community_id=p_community AND e.telegram_user_id=p_user
            AND entitlement_is_current(e.status,e.valid_until,s.auto_renew_state,s.state))
        OR EXISTS(SELECT 1 FROM access_overrides o WHERE o.community_id=p_community
            AND o.telegram_user_id=p_user AND o.active=true AND o.valid_until>now())
$$;

-- A live subscription is one Telegram may still charge, or that is still paid for.
-- Checkout refuses a second one so a member cannot be billed twice.
CREATE FUNCTION member_has_live_subscription(p_community uuid, p_user bigint)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM subscriptions s WHERE s.community_id=p_community
        AND s.telegram_user_id=p_user AND s.state NOT IN ('expired','refunded')
        AND (s.paid_through>now() OR s.auto_renew_state='on'))
$$;
