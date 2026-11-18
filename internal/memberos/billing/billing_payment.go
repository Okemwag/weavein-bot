package billing

import (
	"context"
	"errors"
	"fmt"
	"memberos/internal/eventing"
	"memberos/internal/models"
	"memberos/internal/notify"
	"memberos/internal/store"
	"time"

	"github.com/jackc/pgx/v5"
)

func recordPayment(ctx context.Context, tx pgx.Tx, botID, updateID, seq int64, message paymentMessage) error {
	p := message.Successful
	paidAt := time.Unix(message.Date, 0).UTC()
	until, err := models.PaidThrough(p.Expiration, paidAt)
	if err != nil || message.Date <= 0 || paidAt.After(time.Now().Add(time.Minute)) ||
		p.Currency != "XTR" || !p.Recurring || p.ChargeID == "" || message.From.ID <= 0 {
		return eventing.InvalidEvent("invalid recurring payment fields")
	}
	var communityID, creatorID, checkoutStatus string
	var planVersionID *string
	var buyerID, expectedBot int64
	var price int
	err = tx.QueryRow(ctx, `SELECT community_id,creator_id,telegram_user_id,billing_bot_id,price_stars,status,plan_version_id
        FROM checkout_attempts WHERE invoice_payload=$1 FOR UPDATE`, p.Payload).
		Scan(&communityID, &creatorID, &buyerID, &expectedBot, &price, &checkoutStatus, &planVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.InvalidEvent("unknown invoice payload")
	}
	if err != nil {
		return err
	}
	if expectedBot != botID || buyerID != message.From.ID || price != p.Amount || checkoutStatus == "failed" {
		return eventing.InvalidEvent("payment does not match checkout")
	}
	if err := store.LockAccess(ctx, tx, communityID, buyerID); err != nil {
		return err
	}
	// Paying is a private chat with this bot, so the buyer can receive notices.
	if _, err := tx.Exec(ctx, `INSERT INTO bot_private_contacts(bot_id,telegram_user_id) VALUES($1,$2)
		ON CONFLICT (bot_id,telegram_user_id) DO UPDATE SET last_contact_at=now()`, botID, buyerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO members(creator_id,telegram_user_id,first_name,username)
        VALUES($1,$2,coalesce((SELECT first_name FROM telegram_profiles WHERE telegram_user_id=$2),''),
        coalesce((SELECT username FROM telegram_profiles WHERE telegram_user_id=$2),''))
        ON CONFLICT (creator_id,telegram_user_id) DO NOTHING`, creatorID, buyerID); err != nil {
		return err
	}
	var existingPayload string
	err = tx.QueryRow(ctx, `SELECT invoice_payload FROM payments WHERE billing_bot_id=$1 AND telegram_charge_id=$2`, botID, p.ChargeID).Scan(&existingPayload)
	if err == nil {
		if existingPayload != p.Payload {
			return eventing.InvalidEvent("charge ID belongs to another checkout")
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var subscriptionID string
	var previousUntil time.Time
	err = tx.QueryRow(ctx, `SELECT id,paid_through FROM subscriptions WHERE invoice_payload=$1 FOR UPDATE`, p.Payload).
		Scan(&subscriptionID, &previousUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		if !p.First {
			return eventing.DependencyPending("renewal awaits initial payment")
		}
		var duplicate bool
		if err := tx.QueryRow(ctx, `SELECT member_has_live_subscription($1,$2)`, communityID, buyerID).Scan(&duplicate); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO subscriptions(community_id,creator_id,telegram_user_id,billing_bot_id,invoice_payload,
			state,auto_renew_state,paid_through,first_paid_at,last_payment_at,plan_version_id,last_billing_seq)
			VALUES($1,$2,$3,$4,$5,'active','on',$6,$7,$7,$8,$9)
			RETURNING id`, communityID, creatorID, buyerID, botID, p.Payload, until, paidAt, planVersionID, seq).Scan(&subscriptionID)
		if err != nil {
			return err
		}
		if duplicate {
			if err := flagDuplicateSubscription(ctx, tx, botID, updateID, subscriptionID, communityID, creatorID, buyerID, p.ChargeID); err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	} else {
		if p.First {
			return eventing.InvalidEvent("unexpected second first payment")
		}
		if until.After(previousUntil) {
			_, err = tx.Exec(ctx, `UPDATE subscriptions SET paid_through=$2,last_payment_at=$3,
				state=CASE WHEN $2<=now() THEN 'expired'
					WHEN auto_renew_state='off' THEN 'cancel_scheduled'
					WHEN auto_renew_state='failed' AND last_billing_seq>$4 THEN 'payment_failed'
					ELSE 'active' END,
				auto_renew_state=CASE WHEN auto_renew_state='off' OR last_billing_seq>$4
					THEN auto_renew_state ELSE 'on' END,
				last_billing_seq=CASE WHEN auto_renew_state='off' THEN last_billing_seq
					ELSE GREATEST(last_billing_seq,$4) END,updated_at=now()
				WHERE id=$1`, subscriptionID, until, paidAt, seq)
			if err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO payments(subscription_id,billing_bot_id,telegram_charge_id,invoice_payload,price_stars,
        provider_expiration,paid_at,is_first_recurring) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		subscriptionID, botID, p.ChargeID, p.Payload, p.Amount, until, paidAt, p.First)
	if err != nil {
		return err
	}
	if err := recordSubscriptionEvent(ctx, tx, subscriptionID, "payment:"+subscriptionID+":"+p.ChargeID, "payment_verified",
		map[string]any{"charge_id": p.ChargeID, "amount_stars": p.Amount, "paid_through": until, "first": p.First}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE checkout_attempts SET status='paid' WHERE invoice_payload=$1`, p.Payload)
	if err != nil {
		return err
	}
	if err := refreshEntitlement(ctx, tx, subscriptionID, communityID, buyerID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
        VALUES($1,$2,'approve',$3) ON CONFLICT (dedupe_key) DO NOTHING`, communityID, buyerID, "approve:payment:"+p.ChargeID)
	if err != nil {
		return err
	}
	var title, joinLink, support string
	if err := tx.QueryRow(ctx, `SELECT c.title,coalesce(c.managed_join_link,''),cr.support_contact
        FROM communities c JOIN creators cr ON cr.id=c.creator_id WHERE c.id=$1`, communityID).
		Scan(&title, &joinLink, &support); err != nil {
		return err
	}
	noticeText := fmt.Sprintf("Your renewal for %s went through (%d Stars). You're covered until %s.\n\nNeed help? %s",
		title, p.Amount, until.Format("2 Jan 2006"), support)
	if p.First {
		noticeText = fmt.Sprintf("Payment received, thanks! (%d Stars)\n\nOne step left: tap this link and send the join request to %s. You'll be approved automatically.\n%s\n\nYour membership runs until %s. Need help? %s",
			p.Amount, title, joinLink, until.Format("2 Jan 2006"), support)
	}
	if err := notify.Queue(ctx, tx, botID, buyerID, "payment:"+p.ChargeID, noticeText); err != nil {
		return err
	}
	return AuditEvent(ctx, tx, creatorID, buyerID, "payment_recorded", updateID)
}

// flagDuplicateSubscription handles a second paid subscription that got past the
// checkout guard, such as two invoices paid at the same moment. The charge is real
// and stays recorded; the duplicate's renewal is stopped and the member is told.
func flagDuplicateSubscription(ctx context.Context, tx pgx.Tx, botID, updateID int64, subscriptionID, communityID, creatorID string, buyerID int64, chargeID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key,subscription_id)
		VALUES($1,$2,'cancel_renewal',$3,$4) ON CONFLICT (dedupe_key) DO NOTHING`,
		communityID, buyerID, "cancel-renewal:duplicate:"+chargeID, subscriptionID); err != nil {
		return err
	}
	if err := notify.Queue(ctx, tx, botID, buyerID, "duplicate-subscription:"+chargeID,
		"It looks like you paid twice for the same community. We turned off the extra renewal so it won't charge you again. To get the extra payment back, open MemberOS and tap Payments and refunds."); err != nil {
		return err
	}
	return AuditEvent(ctx, tx, creatorID, buyerID, "duplicate_subscription_detected", updateID)
}
