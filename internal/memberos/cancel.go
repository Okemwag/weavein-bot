package memberos

import (
	"context"
	"errors"
	"log/slog"
	"memberos/internal/memberos/billing"
	"memberos/internal/notify"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// cancelSubscription disables the bot-managed renewal. Telegram remains the
// payment authority and access lasts through the already paid period. Telegram
// is called outside any transaction, and its later subscription update corrects
// the record if the local write below does not complete.
func (a *app) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var subscriptionID, creatorID, chargeID, state, renew string
	var botID int64
	var through time.Time
	err = a.db.QueryRow(r.Context(), `SELECT s.id,s.creator_id,s.billing_bot_id,s.state,s.auto_renew_state,s.paid_through,
        coalesce((SELECT p.telegram_charge_id FROM payments p WHERE p.subscription_id=s.id
         AND p.is_first_recurring=true ORDER BY p.paid_at ASC LIMIT 1),'')
        FROM subscriptions s WHERE s.id=$1 AND s.telegram_user_id=$2`,
		r.PathValue("id"), userID).Scan(&subscriptionID, &creatorID, &botID, &state, &renew, &through, &chargeID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if botID != a.cfg.botID {
		http.Error(w, "billing bot unavailable", http.StatusServiceUnavailable)
		return
	}
	if !through.After(time.Now()) || state == "refunded" || state == "expired" {
		http.Error(w, "This membership has already ended", http.StatusConflict)
		return
	}
	if renew == "off" {
		writeJSON(w, 200, map[string]any{"state": state, "auto_renew_state": renew, "paid_through": through})
		return
	}
	if chargeID == "" {
		http.Error(w, "We can't find this payment. Please contact support", http.StatusConflict)
		return
	}
	// A subscriber closing the app must not lose the record of a cancellation
	// that Telegram has already applied.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()
	var ok bool
	err = a.telegram.Call(ctx, "editUserStarSubscription", url.Values{
		"user_id": {strconv.FormatInt(userID, 10)}, "telegram_payment_charge_id": {chargeID}, "is_canceled": {"true"},
	}, &ok)
	if err != nil || !ok {
		http.Error(w, "Telegram couldn't cancel your renewal. Please try again in a moment", http.StatusBadGateway)
		return
	}
	if err := a.recordCancellation(ctx, subscriptionID, creatorID, botID, userID, through); err != nil {
		slog.Error("renewal canceled at Telegram but not recorded", "subscription_id", subscriptionID, "error", err)
		http.Error(w, "Your renewal is cancelled. This page will update shortly", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, 200, map[string]any{"state": "cancel_scheduled", "auto_renew_state": "off", "paid_through": through})
}

func (a *app) recordCancellation(ctx context.Context, subscriptionID, creatorID string, botID, userID int64, through time.Time) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE subscriptions SET auto_renew_state='off',canceled_at=now(),updated_at=now(),
        state=CASE WHEN state IN ('refunded','expired') THEN state ELSE 'cancel_scheduled' END WHERE id=$1`, subscriptionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO subscription_events(subscription_id,event_key,kind,details)
        VALUES($1,$2,'cancellation_requested',jsonb_build_object('paid_through',$3::timestamptz))
        ON CONFLICT (event_key) DO NOTHING`, subscriptionID, "cancel:"+subscriptionID, through); err != nil {
		return err
	}
	if err := notify.Queue(ctx, tx, botID, userID, "cancel:"+subscriptionID,
		"Your renewal is cancelled, so you won't be charged again. You keep access until "+through.Format("2 Jan 2006")+"."); err != nil {
		return err
	}
	if err := billing.AuditEvent(ctx, tx, creatorID, userID, "subscription_canceled_by_member", 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
