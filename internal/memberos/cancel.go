package memberos

import (
	"errors"
	"memberos/internal/memberos/billing"
	"memberos/internal/notify"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// cancelSubscription disables the bot-managed renewal. Telegram remains the
// payment authority and access lasts through the already paid period.
func (a *app) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var subscriptionID, creatorID, chargeID, state, renew string
	var botID int64
	var through time.Time
	err = tx.QueryRow(r.Context(), `SELECT s.id,s.creator_id,s.billing_bot_id,s.state,s.auto_renew_state,s.paid_through,
        (SELECT p.telegram_charge_id FROM payments p WHERE p.subscription_id=s.id
         AND p.is_first_recurring=true ORDER BY p.paid_at ASC LIMIT 1)
        FROM subscriptions s WHERE s.id::text=$1 AND s.telegram_user_id=$2 FOR UPDATE OF s`,
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
		http.Error(w, "subscription is not active", http.StatusConflict)
		return
	}
	if renew == "off" {
		writeJSON(w, 200, map[string]any{"state": state, "auto_renew_state": renew, "paid_through": through})
		return
	}
	var ok bool
	err = a.telegram.Call(r.Context(), "editUserStarSubscription", url.Values{
		"user_id": {strconv.FormatInt(userID, 10)}, "telegram_payment_charge_id": {chargeID}, "is_canceled": {"true"},
	}, &ok)
	if err != nil || !ok {
		http.Error(w, "Telegram could not cancel renewal", http.StatusBadGateway)
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE subscriptions SET state='cancel_scheduled',auto_renew_state='off',
        canceled_at=now(),updated_at=now() WHERE id=$1`, subscriptionID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO subscription_events(subscription_id,event_key,kind,details)
        VALUES($1,$2,'cancellation_requested',jsonb_build_object('paid_through',$3))
        ON CONFLICT (event_key) DO NOTHING`, subscriptionID, "cancel:"+subscriptionID, through)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := notify.Queue(r.Context(), tx, botID, userID, "cancel:"+subscriptionID,
		"Renewal is off. Your paid access remains available through "+through.Format("2 Jan 2006")+"."); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := billing.AuditEvent(r.Context(), tx, creatorID, userID, "subscription_canceled_by_member", 0); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"state": "cancel_scheduled", "auto_renew_state": "off", "paid_through": through})
}
