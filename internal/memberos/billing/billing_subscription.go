package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"memberos/internal/eventing"
	"memberos/internal/notify"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func ProcessSubscription(ctx context.Context, tx pgx.Tx, botID, updateID, seq int64, raw []byte) error {
	var wrapper struct {
		Subscription subscriptionChange `json:"subscription"`
	}
	if json.Unmarshal(raw, &wrapper) != nil {
		return eventing.InvalidEvent("invalid subscription JSON")
	}
	change := wrapper.Subscription
	if change.User.ID <= 0 || (change.State != "canceled" && change.State != "active" && change.State != "failed") {
		return eventing.InvalidEvent("invalid subscription change")
	}
	var subscriptionID, creatorID string
	var buyerID, expectedBot int64
	var until time.Time
	var lastSeq int64
	err := tx.QueryRow(ctx, `SELECT id,creator_id,telegram_user_id,billing_bot_id,paid_through,last_billing_seq
        FROM subscriptions WHERE invoice_payload=$1 FOR UPDATE`, change.Payload).
		Scan(&subscriptionID, &creatorID, &buyerID, &expectedBot, &until, &lastSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.DependencyPending("subscription update awaits initial payment")
	}
	if err != nil {
		return err
	}
	if buyerID != change.User.ID || expectedBot != botID {
		return eventing.InvalidEvent("subscription update identity mismatch")
	}
	if seq <= lastSeq {
		return nil
	}
	state, renew := "active", "on"
	if change.State == "canceled" {
		state, renew = "cancel_scheduled", "off"
	}
	if change.State == "failed" {
		state, renew = "payment_failed", "failed"
	}
	if !until.After(time.Now()) {
		state = "expired"
	}
	_, err = tx.Exec(ctx, `UPDATE subscriptions SET state=CASE WHEN state='refunded' THEN state ELSE $2 END,auto_renew_state=$3,
        last_failed_at=CASE WHEN $4='failed' THEN now() ELSE last_failed_at END,
        canceled_at=CASE WHEN $4='canceled' THEN now() ELSE canceled_at END,
		last_subscription_update_id=$5,last_billing_seq=$6,updated_at=now() WHERE id=$1`, subscriptionID, state, renew, change.State, updateID, seq)
	if err != nil {
		return err
	}
	if err := recordSubscriptionEvent(ctx, tx, subscriptionID,
		"subscription:"+change.Payload+":"+strconv.FormatInt(updateID, 10), "subscription_"+change.State,
		map[string]any{"paid_through": until, "state": state}); err != nil {
		return err
	}
	var message string
	switch change.State {
	case "canceled":
		message = fmt.Sprintf("Your renewal is cancelled, so you won't be charged again. You keep access until %s.", until.Format("2 Jan 2006"))
	case "failed":
		message = fmt.Sprintf("Your renewal didn't go through. You keep access until %s. Make sure you have enough Stars, or open MemberOS for help.", until.Format("2 Jan 2006"))
	case "active":
		message = fmt.Sprintf("Your renewal is back on. You're covered until %s.", until.Format("2 Jan 2006"))
	}
	if err := notify.Queue(ctx, tx, botID, buyerID, fmt.Sprintf("subscription:%s:%d", subscriptionID, updateID), message); err != nil {
		return err
	}
	return AuditEvent(ctx, tx, creatorID, buyerID, "subscription_"+change.State, updateID)
}
