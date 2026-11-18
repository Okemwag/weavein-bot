package billing

import (
	"context"
	"errors"
	"memberos/internal/eventing"
	"memberos/internal/notify"
	"memberos/internal/store"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func recordRefund(ctx context.Context, tx pgx.Tx, botID, updateID, seq int64, message paymentMessage) error {
	p := message.Refunded
	if p.Currency != "XTR" || p.ChargeID == "" || p.Amount <= 0 {
		return eventing.InvalidEvent("invalid refund fields")
	}
	var paymentID, subscriptionID, communityID, creatorID, payload string
	var buyerID, expectedBot int64
	var amount int
	var refundedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT p.id,p.subscription_id,s.community_id,s.creator_id,s.telegram_user_id,
        p.billing_bot_id,p.invoice_payload,p.price_stars,p.refunded_at
        FROM payments p JOIN subscriptions s ON s.id=p.subscription_id
        WHERE p.billing_bot_id=$1 AND p.telegram_charge_id=$2 FOR UPDATE OF p,s`, botID, p.ChargeID).
		Scan(&paymentID, &subscriptionID, &communityID, &creatorID, &buyerID, &expectedBot, &payload, &amount, &refundedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.DependencyPending("refund awaits original charge")
	}
	if err != nil {
		return err
	}
	if expectedBot != botID || payload != p.Payload || amount != p.Amount {
		return eventing.InvalidEvent("refund does not match charge")
	}
	if err := store.LockAccess(ctx, tx, communityID, buyerID); err != nil {
		return err
	}
	if refundedAt != nil {
		return nil
	}
	if err := recordSubscriptionEvent(ctx, tx, subscriptionID, "refund:"+subscriptionID+":"+p.ChargeID,
		"payment_refunded", map[string]any{"charge_id": p.ChargeID, "amount_stars": p.Amount}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE refund_requests SET status='confirmed',updated_at=now()
        WHERE payment_id=$1 AND status IN ('requested','processing','submitted','needs_review')`, paymentID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE payments SET refunded_at=now() WHERE id=$1`, paymentID)
	if err != nil {
		return err
	}
	if err := refreshEntitlement(ctx, tx, subscriptionID, communityID, buyerID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE subscriptions SET state=CASE WHEN EXISTS(SELECT 1 FROM entitlements
	        WHERE subscription_id=$1 AND status='active') THEN
			CASE WHEN auto_renew_state='off' THEN 'cancel_scheduled'
				WHEN auto_renew_state='failed' THEN 'payment_failed' ELSE 'active' END
			ELSE 'refunded' END,
		last_billing_seq=GREATEST(last_billing_seq,$2),updated_at=now() WHERE id=$1`, subscriptionID, seq)
	if err != nil {
		return err
	}
	// A refund that ends access must also stop the next charge. Refunding an
	// older payment while a later one is still current leaves renewal alone.
	_, err = tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key,subscription_id)
		SELECT s.community_id,s.telegram_user_id,'cancel_renewal',$2,s.id FROM subscriptions s
		WHERE s.id=$1 AND s.state='refunded' AND s.auto_renew_state='on'
		ON CONFLICT (dedupe_key) DO NOTHING`, subscriptionID, "cancel-renewal:refund:"+p.ChargeID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
        VALUES($1,$2,'remove',$3) ON CONFLICT (dedupe_key) DO NOTHING`, communityID, buyerID, "remove:refund:"+p.ChargeID)
	if err != nil {
		return err
	}
	if err := notify.Queue(ctx, tx, botID, buyerID, "refund:"+p.ChargeID,
		"Your "+strconv.Itoa(p.Amount)+" Stars refund is confirmed. Open MemberOS to see your membership, or to get help."); err != nil {
		return err
	}
	return AuditEvent(ctx, tx, creatorID, buyerID, "payment_refunded", updateID)
}
