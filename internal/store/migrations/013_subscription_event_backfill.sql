-- Reconstruct only facts that still exist in authoritative financial rows.
-- Historical failures and cancellation transitions cannot be inferred safely.
INSERT INTO subscription_events(subscription_id,event_key,kind,occurred_at,details)
SELECT p.subscription_id,
       'payment:' || p.subscription_id::text || ':' || p.telegram_charge_id,
       'payment_verified',p.paid_at,
       jsonb_build_object('charge_id',p.telegram_charge_id,'amount_stars',p.price_stars,
                          'paid_through',p.provider_expiration,'first',p.is_first_recurring)
FROM payments p
ON CONFLICT (event_key) DO NOTHING;

INSERT INTO subscription_events(subscription_id,event_key,kind,occurred_at,details)
SELECT p.subscription_id,
       'refund:' || p.subscription_id::text || ':' || p.telegram_charge_id,
       'payment_refunded',p.refunded_at,
       jsonb_build_object('charge_id',p.telegram_charge_id,'refunded_at',p.refunded_at)
FROM payments p WHERE p.refunded_at IS NOT NULL
ON CONFLICT (event_key) DO NOTHING;
