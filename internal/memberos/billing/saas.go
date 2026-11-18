package billing

import (
	"context"
	"errors"
	"fmt"
	"memberos/internal/eventing"
	"memberos/internal/models"
	"memberos/internal/notify"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// SaaSPayloadPrefix marks invoices for a creator's own MemberOS Pro plan. These
// never touch community subscriptions, entitlements or the creator Stars ledger.
const SaaSPayloadPrefix = "saas:"

// IsSaaSPayload reports whether an invoice payload belongs to MemberOS Pro.
func IsSaaSPayload(payload string) bool { return strings.HasPrefix(payload, SaaSPayloadPrefix) }

func recordSaaSPayment(ctx context.Context, tx pgx.Tx, botID, updateID, seq int64, message paymentMessage) error {
	p := message.Successful
	paidAt := time.Unix(message.Date, 0).UTC()
	until, err := models.PaidThrough(p.Expiration, paidAt)
	if err != nil || message.Date <= 0 || paidAt.After(time.Now().Add(time.Minute)) ||
		p.Currency != "XTR" || !p.Recurring || p.ChargeID == "" || message.From.ID <= 0 {
		return eventing.InvalidEvent("invalid MemberOS Pro payment fields")
	}
	var workspaceID string
	var payer, expectedBot int64
	var price, limit int
	var checkoutStatus string
	err = tx.QueryRow(ctx, `SELECT workspace_id,telegram_user_id,billing_bot_id,price_stars,community_limit,status
		FROM saas_checkouts WHERE invoice_payload=$1 FOR UPDATE`, p.Payload).
		Scan(&workspaceID, &payer, &expectedBot, &price, &limit, &checkoutStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.InvalidEvent("unknown MemberOS Pro invoice")
	}
	if err != nil {
		return err
	}
	if expectedBot != botID || payer != message.From.ID || price != p.Amount || checkoutStatus == "failed" {
		return eventing.InvalidEvent("MemberOS Pro payment does not match checkout")
	}
	// The account row serializes every Pro change for this workspace.
	var plan string
	if err := tx.QueryRow(ctx, `SELECT plan_code FROM saas_accounts WHERE workspace_id=$1 FOR UPDATE`, workspaceID).Scan(&plan); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bot_private_contacts(bot_id,telegram_user_id) VALUES($1,$2)
		ON CONFLICT (bot_id,telegram_user_id) DO UPDATE SET last_contact_at=now()`, botID, payer); err != nil {
		return err
	}
	var existingPayload string
	err = tx.QueryRow(ctx, `SELECT s.invoice_payload FROM saas_payments sp JOIN saas_subscriptions s ON s.id=sp.saas_subscription_id
		WHERE sp.billing_bot_id=$1 AND sp.telegram_charge_id=$2`, botID, p.ChargeID).Scan(&existingPayload)
	if err == nil {
		if existingPayload != p.Payload {
			return eventing.InvalidEvent("charge ID belongs to another MemberOS Pro checkout")
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var subscriptionID string
	var previousUntil time.Time
	duplicate := false
	err = tx.QueryRow(ctx, `SELECT id,paid_through FROM saas_subscriptions WHERE invoice_payload=$1 FOR UPDATE`, p.Payload).
		Scan(&subscriptionID, &previousUntil)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if !p.First {
			return eventing.DependencyPending("MemberOS Pro renewal awaits initial payment")
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM saas_subscriptions WHERE workspace_id=$1
			AND state IN ('active','cancel_scheduled','payment_failed') AND paid_through>now())`, workspaceID).Scan(&duplicate); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO saas_subscriptions(workspace_id,invoice_payload,telegram_user_id,billing_bot_id,
			first_charge_id,community_limit,state,paid_through,last_billing_seq) VALUES($1,$2,$3,$4,$5,$6,'active',$7,$8) RETURNING id`,
			workspaceID, p.Payload, payer, botID, p.ChargeID, limit, until, seq).Scan(&subscriptionID)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if p.First {
			return eventing.InvalidEvent("unexpected second first MemberOS Pro payment")
		}
		if until.After(previousUntil) {
			if _, err := tx.Exec(ctx, `UPDATE saas_subscriptions SET paid_through=$2,
				state=CASE WHEN state IN ('refunded','cancel_scheduled') THEN state ELSE 'active' END,
				last_billing_seq=GREATEST(last_billing_seq,$3),updated_at=now() WHERE id=$1`, subscriptionID, until, seq); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO saas_payments(saas_subscription_id,billing_bot_id,telegram_charge_id,price_stars,paid_at,paid_through)
		VALUES($1,$2,$3,$4,$5,$6)`, subscriptionID, botID, p.ChargeID, p.Amount, paidAt, until); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO saas_financial_events(workspace_id,provider,provider_event_id,amount_minor,currency,kind,occurred_at)
		VALUES($1,'telegram_stars',$2,$3,'XTR','charge',$4) ON CONFLICT (provider,provider_event_id) DO NOTHING`,
		workspaceID, strconv.FormatInt(botID, 10)+":"+p.ChargeID, p.Amount, paidAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE saas_checkouts SET status='paid' WHERE invoice_payload=$1`, p.Payload); err != nil {
		return err
	}
	// A pilot grant is already at least as generous, so it is left alone.
	if _, err := tx.Exec(ctx, `UPDATE saas_accounts SET
		plan_code=CASE WHEN plan_code='pilot' THEN plan_code ELSE 'pro' END,
		community_limit=CASE WHEN plan_code='pilot' THEN community_limit ELSE GREATEST(community_limit,$2) END,
		pro_paid_through=GREATEST(coalesce(pro_paid_through,$3),$3),lapsed_at=NULL,updated_at=now()
		WHERE workspace_id=$1`, workspaceID, limit, until); err != nil {
		return err
	}
	text := fmt.Sprintf("MemberOS Pro is active (%d Stars). You can connect up to %d communities. Paid until %s.",
		p.Amount, limit, until.Format("2 Jan 2006"))
	if !p.First {
		text = fmt.Sprintf("Your MemberOS Pro renewal went through (%d Stars). Paid until %s.", p.Amount, until.Format("2 Jan 2006"))
	}
	if duplicate {
		text += "\n\nIt looks like you started a second Pro subscription. Send /paysupport and we'll refund the extra one."
	}
	if err := notify.Queue(ctx, tx, botID, payer, "saas-payment:"+p.ChargeID, text); err != nil {
		return err
	}
	action := "saas_payment_recorded"
	if duplicate {
		action = "saas_duplicate_subscription"
	}
	return saasAudit(ctx, tx, payer, action, workspaceID, updateID)
}

func recordSaaSRefund(ctx context.Context, tx pgx.Tx, botID, updateID int64, message paymentMessage) error {
	p := message.Refunded
	if p.Currency != "XTR" || p.ChargeID == "" || p.Amount <= 0 {
		return eventing.InvalidEvent("invalid MemberOS Pro refund fields")
	}
	var paymentID, subscriptionID, workspaceID, payload string
	var payer int64
	var amount int
	var refundedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT sp.id,s.id,s.workspace_id,s.invoice_payload,s.telegram_user_id,sp.price_stars,sp.refunded_at
		FROM saas_payments sp JOIN saas_subscriptions s ON s.id=sp.saas_subscription_id
		WHERE sp.billing_bot_id=$1 AND sp.telegram_charge_id=$2 FOR UPDATE OF sp,s`, botID, p.ChargeID).
		Scan(&paymentID, &subscriptionID, &workspaceID, &payload, &payer, &amount, &refundedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.DependencyPending("MemberOS Pro refund awaits original charge")
	}
	if err != nil {
		return err
	}
	if payload != p.Payload || amount != p.Amount {
		return eventing.InvalidEvent("MemberOS Pro refund does not match charge")
	}
	if refundedAt != nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM saas_accounts WHERE workspace_id=$1 FOR UPDATE`, workspaceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE saas_payments SET refunded_at=now(),refund_status='submitted' WHERE id=$1`, paymentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE saas_subscriptions SET state='refunded',updated_at=now() WHERE id=$1`, subscriptionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO saas_financial_events(workspace_id,provider,provider_event_id,amount_minor,currency,kind,occurred_at)
		VALUES($1,'telegram_stars',$2,$3,'XTR','refund',now()) ON CONFLICT (provider,provider_event_id) DO NOTHING`,
		workspaceID, "refund:"+strconv.FormatInt(botID, 10)+":"+p.ChargeID, p.Amount); err != nil {
		return err
	}
	// A refunded plan ends now unless another paid subscription still covers it.
	if _, err := tx.Exec(ctx, `UPDATE saas_accounts sa SET pro_paid_through=covered.until,
		plan_code=CASE WHEN covered.until IS NULL AND plan_code='pro' THEN 'free' ELSE plan_code END,
		community_limit=CASE WHEN covered.until IS NULL AND plan_code='pro' THEN 1 ELSE community_limit END,
		lapsed_at=CASE WHEN covered.until IS NULL AND plan_code='pro' THEN now() ELSE lapsed_at END,updated_at=now()
		FROM (SELECT max(paid_through) AS until FROM saas_subscriptions WHERE workspace_id=$1
			AND state IN ('active','cancel_scheduled','payment_failed') AND paid_through>now()) covered
		WHERE sa.workspace_id=$1`, workspaceID); err != nil {
		return err
	}
	if err := notify.Queue(ctx, tx, botID, payer, "saas-refund:"+p.ChargeID, fmt.Sprintf(
		"We refunded %d Stars for MemberOS Pro. Your communities keep running and none of your data was removed; you can't add new communities beyond the Free plan until you upgrade again.", p.Amount)); err != nil {
		return err
	}
	return saasAudit(ctx, tx, payer, "saas_refund_recorded", workspaceID, updateID)
}

func processSaaSSubscription(ctx context.Context, tx pgx.Tx, botID, updateID, seq int64, change subscriptionChange) error {
	var subscriptionID, workspaceID string
	var payer, expectedBot, lastSeq int64
	var until time.Time
	var current string
	err := tx.QueryRow(ctx, `SELECT id,workspace_id,telegram_user_id,billing_bot_id,paid_through,last_billing_seq,state
		FROM saas_subscriptions WHERE invoice_payload=$1 FOR UPDATE`, change.Payload).
		Scan(&subscriptionID, &workspaceID, &payer, &expectedBot, &until, &lastSeq, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.DependencyPending("MemberOS Pro update awaits initial payment")
	}
	if err != nil {
		return err
	}
	if payer != change.User.ID || expectedBot != botID {
		return eventing.InvalidEvent("MemberOS Pro update identity mismatch")
	}
	if seq <= lastSeq || current == "refunded" {
		return nil
	}
	state := map[string]string{"canceled": "cancel_scheduled", "failed": "payment_failed", "active": "active"}[change.State]
	if !until.After(time.Now()) {
		state = "expired"
	}
	if _, err := tx.Exec(ctx, `UPDATE saas_subscriptions SET state=$2,last_billing_seq=$3,updated_at=now() WHERE id=$1`,
		subscriptionID, state, seq); err != nil {
		return err
	}
	date := until.Format("2 Jan 2006")
	message := map[string]string{
		"canceled": "MemberOS Pro won't renew. Everything keeps working until " + date + ". After that your communities keep running, but you can't add new ones beyond the Free plan.",
		"failed":   "Your MemberOS Pro renewal didn't go through. Pro stays on until " + date + " plus 7 days. Top up your Stars, or open MemberOS > Settings to try again.",
		"active":   "MemberOS Pro renewal is back on. Paid until " + date + ".",
	}[change.State]
	if err := notify.Queue(ctx, tx, botID, payer, fmt.Sprintf("saas-subscription:%s:%d", subscriptionID, updateID), message); err != nil {
		return err
	}
	return saasAudit(ctx, tx, payer, "saas_subscription_"+change.State, workspaceID, updateID)
}

func saasAudit(ctx context.Context, tx pgx.Tx, user int64, action, workspaceID string, updateID int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,$2,jsonb_build_object('workspace_id',$3::uuid,'update_id',$4::bigint))`, user, action, workspaceID, updateID)
	return err
}
