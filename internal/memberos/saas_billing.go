package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"memberos/internal/memberos/billing"
	"memberos/internal/notify"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// saasGraceDays is how long Pro keeps its allowance after the paid period ends,
// so a late Telegram renewal or a short Stars shortfall changes nothing.
const saasGraceDays = 7

type saasOwnerAccount struct {
	workspace, plan, status string
	limit, used             int
}

// saasOwner loads the workspace account when the session user owns it.
func (a *app) saasOwner(w http.ResponseWriter, r *http.Request, user int64, lock bool, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (saasOwnerAccount, bool) {
	var acc saasOwnerAccount
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF sa"
	}
	err := q.QueryRow(r.Context(), `SELECT sa.workspace_id,sa.plan_code,sa.status,sa.community_limit,
		(SELECT count(*) FROM communities c WHERE c.workspace_id=sa.workspace_id AND c.status<>'archived')
		FROM saas_accounts sa JOIN workspace_members wm ON wm.workspace_id=sa.workspace_id
		WHERE sa.workspace_id=$1 AND wm.telegram_user_id=$2 AND wm.role='owner'`+suffix, r.PathValue("id"), user).
		Scan(&acc.workspace, &acc.plan, &acc.status, &acc.limit, &acc.used)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return acc, false
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return acc, false
	}
	return acc, true
}

// saasCheckout creates a recurring Stars invoice for MemberOS Pro, paid to the
// MemberOS bot. Pilot accounts are operator-granted and never asked to pay.
func (a *app) saasCheckout(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !a.cfg.saasEnabled {
		http.Error(w, "MemberOS Pro isn't available yet", http.StatusServiceUnavailable)
		return
	}
	payload, err := randomPayload()
	if err != nil {
		http.Error(w, "checkout unavailable", http.StatusServiceUnavailable)
		return
	}
	payload = billing.SaaSPayloadPrefix + payload
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	acc, ok := a.saasOwner(w, r, user, true, tx)
	if !ok {
		return
	}
	if acc.status != "active" {
		http.Error(w, "Your account is paused. Please contact the MemberOS team", http.StatusConflict)
		return
	}
	if acc.plan == "pilot" {
		http.Error(w, "You're on an early access plan, so there's nothing to pay", http.StatusConflict)
		return
	}
	var live bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM saas_subscriptions WHERE workspace_id=$1
		AND state IN ('active','cancel_scheduled','payment_failed') AND paid_through>now())`, acc.workspace).Scan(&live); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if live {
		http.Error(w, "You already have MemberOS Pro, so you won't be charged again", http.StatusConflict)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO saas_checkouts(invoice_payload,workspace_id,telegram_user_id,billing_bot_id,
		price_stars,community_limit,expires_at) VALUES($1,$2,$3,$4,$5,$6,now()+interval '30 minutes')`,
		payload, acc.workspace, user, a.cfg.botID, a.cfg.saasProPrice, a.cfg.saasProLimit); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	prices, _ := json.Marshal([]map[string]any{{"label": "MemberOS Pro", "amount": a.cfg.saasProPrice}})
	var invoiceURL string
	err = a.telegram.Call(r.Context(), "createInvoiceLink", url.Values{
		"title": {"MemberOS Pro"}, "description": {fmt.Sprintf("Connect up to %d communities. Renews every 30 days; cancel anytime.", a.cfg.saasProLimit)},
		"payload": {payload}, "currency": {"XTR"}, "prices": {string(prices)}, "subscription_period": {"2592000"},
	}, &invoiceURL)
	if err != nil || invoiceURL == "" {
		_, _ = a.db.Exec(context.WithoutCancel(r.Context()), `UPDATE saas_checkouts SET status='failed' WHERE invoice_payload=$1 AND status='pending'`, payload)
		http.Error(w, "Telegram couldn't create your invoice. Please try again in a moment", http.StatusBadGateway)
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE saas_checkouts SET invoice_url=$2 WHERE invoice_payload=$1`, payload, invoiceURL); err != nil {
		http.Error(w, "checkout storage unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, 201, map[string]any{"invoice_url": invoiceURL, "price_stars": a.cfg.saasProPrice,
		"community_limit": a.cfg.saasProLimit, "currency": "XTR", "expires_in": 1800})
}

// saasRenewal turns MemberOS Pro renewal off (cancel) or back on (resume).
// Only the person who paid can change it, because Telegram ties the
// subscription to their account.
func (a *app) saasRenewal(cancel bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.sessionUser(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		acc, ok := a.saasOwner(w, r, user, false, a.db)
		if !ok {
			return
		}
		var subscriptionID, charge, state string
		var payer, bot int64
		var through time.Time
		err = a.db.QueryRow(r.Context(), `SELECT id,first_charge_id,telegram_user_id,billing_bot_id,state,paid_through
			FROM saas_subscriptions WHERE workspace_id=$1 AND state IN ('active','cancel_scheduled','payment_failed')
			AND paid_through>now() ORDER BY paid_through DESC LIMIT 1`, acc.workspace).
			Scan(&subscriptionID, &charge, &payer, &bot, &state, &through)
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "You don't have an active MemberOS Pro subscription", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		if payer != user {
			http.Error(w, "Only the person who paid for Pro can change its renewal", http.StatusForbidden)
			return
		}
		if bot != a.cfg.botID {
			http.Error(w, "billing bot unavailable", http.StatusServiceUnavailable)
			return
		}
		want := map[bool]string{true: "cancel_scheduled", false: "active"}[cancel]
		if state == want {
			writeJSON(w, 200, map[string]any{"state": state, "paid_through": through})
			return
		}
		ctx, stop := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
		defer stop()
		var done bool
		err = a.telegram.Call(ctx, "editUserStarSubscription", url.Values{"user_id": {strconv.FormatInt(user, 10)},
			"telegram_payment_charge_id": {charge}, "is_canceled": {strconv.FormatBool(cancel)}}, &done)
		if err != nil || !done {
			http.Error(w, "Telegram couldn't change your renewal. Please try again in a moment", http.StatusBadGateway)
			return
		}
		// Telegram's own subscription update will confirm this if the write fails.
		if _, err := a.db.Exec(ctx, `UPDATE saas_subscriptions SET state=$2,updated_at=now() WHERE id=$1 AND state<>'refunded'`, subscriptionID, want); err != nil {
			slog.Error("Pro renewal changed at Telegram but not recorded", "subscription_id", subscriptionID, "error", err)
		}
		if _, err := a.db.Exec(ctx, `INSERT INTO audit_events(subject_telegram_user_id,action,details)
			VALUES($1,$2,jsonb_build_object('workspace_id',$3::uuid))`, user, "saas_renewal_"+map[bool]string{true: "canceled", false: "resumed"}[cancel], acc.workspace); err != nil {
			slog.Error("Pro renewal audit failed", "workspace_id", acc.workspace, "error", err)
		}
		writeJSON(w, 200, map[string]any{"state": want, "paid_through": through})
	}
}

// runSaaSLifecycle closes ended Pro subscriptions, warns owners whose payment is
// late, and returns lapsed accounts to Free limits. Nothing is archived or
// deleted: existing communities keep running and only new ones are blocked.
func (a *app) runSaaSLifecycle(ctx context.Context) error {
	if _, err := a.db.Exec(ctx, `UPDATE saas_subscriptions SET state='expired',updated_at=now()
		WHERE state IN ('active','cancel_scheduled','payment_failed') AND paid_through<now()-make_interval(days => $1)`, saasGraceDays); err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT sa.workspace_id,sa.pro_paid_through,
		sa.pro_paid_through<now()-make_interval(days => $1) AS lapsed
		FROM saas_accounts sa WHERE sa.plan_code='pro' AND sa.pro_paid_through<now() FOR UPDATE`, saasGraceDays)
	if err != nil {
		return err
	}
	type due struct {
		workspace string
		through   time.Time
		lapsed    bool
	}
	var items []due
	for rows.Next() {
		var item due
		if err := rows.Scan(&item.workspace, &item.through, &item.lapsed); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for _, item := range items {
		deadline := item.through.AddDate(0, 0, saasGraceDays).Format("2 Jan 2006")
		key := fmt.Sprintf("saas-late:%s:%d", item.workspace, item.through.Unix())
		text := "Your MemberOS Pro period ended on " + item.through.Format("2 Jan 2006") + ". Your communities keep working; renew before " + deadline +
			" to keep your Pro allowance. Open MemberOS > Settings."
		if item.lapsed {
			if _, err := tx.Exec(ctx, `UPDATE saas_accounts SET plan_code='free',community_limit=1,lapsed_at=now(),updated_at=now()
				WHERE workspace_id=$1`, item.workspace); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO audit_events(action,details) VALUES('saas_pro_lapsed',jsonb_build_object('workspace_id',$1::uuid))`, item.workspace); err != nil {
				return err
			}
			key = fmt.Sprintf("saas-lapsed:%s:%d", item.workspace, item.through.Unix())
			text = "MemberOS Pro has ended. Your communities, members and history are all still here and keep working, but you can't add new communities beyond the Free plan. Upgrade again anytime in MemberOS > Settings."
		}
		if err := a.queueOwnerNotice(ctx, tx, item.workspace, key, text); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a *app) queueOwnerNotice(ctx context.Context, tx pgx.Tx, workspace, key, text string) error {
	rows, err := tx.Query(ctx, `SELECT telegram_user_id FROM workspace_members WHERE workspace_id=$1 AND role='owner'`, workspace)
	if err != nil {
		return err
	}
	var owners []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		owners = append(owners, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for _, owner := range owners {
		if err := notify.Queue(ctx, tx, a.cfg.botID, owner, key+":"+strconv.FormatInt(owner, 10), text); err != nil {
			return err
		}
	}
	return nil
}

// adminRefundSaaSPayment refunds one MemberOS Pro charge. The claim is taken
// once: an uncertain Telegram answer leaves it in review, never retried blindly.
func (a *app) adminRefundSaaSPayment(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Reason) < 10 || len(input.Reason) > 500 {
		http.Error(w, "a refund reason of 10 to 500 characters is required", 400)
		return
	}
	var charge string
	var payer, bot int64
	err := a.db.QueryRow(r.Context(), `UPDATE saas_payments sp SET refund_status='needs_review',refund_requested_by=$2,refund_reason=$3
		FROM saas_subscriptions s WHERE sp.id=$1 AND s.id=sp.saas_subscription_id AND sp.refund_status IS NULL AND sp.refunded_at IS NULL
		RETURNING sp.telegram_charge_id,s.telegram_user_id,sp.billing_bot_id`, r.PathValue("id"), operator, input.Reason).Scan(&charge, &payer, &bot)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "This payment is unknown, already refunded, or already has a refund in review", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if bot != a.cfg.botID {
		http.Error(w, "billing bot mismatch; left in review", http.StatusConflict)
		return
	}
	var accepted bool
	err = a.telegram.Call(r.Context(), "refundStarPayment", url.Values{"user_id": {strconv.FormatInt(payer, 10)}, "telegram_payment_charge_id": {charge}}, &accepted)
	outcome := "needs_review"
	if err == nil && accepted {
		outcome = "submitted"
		if _, err := a.db.Exec(context.WithoutCancel(r.Context()), `UPDATE saas_payments SET refund_status='submitted' WHERE id=$1 AND refunded_at IS NULL`, r.PathValue("id")); err != nil {
			slog.Error("Pro refund submitted but not recorded", "payment_id", r.PathValue("id"), "error", err)
		}
	}
	if _, err := a.db.Exec(context.WithoutCancel(r.Context()), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,'saas_refund_approval',jsonb_build_object('payment_id',$2::uuid,'outcome',$3::text,'reason',$4::text))`,
		operator, r.PathValue("id"), outcome, input.Reason); err != nil {
		slog.Error("Pro refund audit failed", "payment_id", r.PathValue("id"), "error", err)
	}
	if outcome != "submitted" {
		http.Error(w, "Telegram didn't confirm the refund. It's marked for review; check the transaction before doing anything else", http.StatusBadGateway)
		return
	}
	writeJSON(w, 200, map[string]string{"status": outcome})
}

// adminSaaSPayments lists recent MemberOS Pro charges for Ops.
func (a *app) adminSaaSPayments(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT sp.id,s.workspace_id,w.name,s.telegram_user_id,sp.price_stars,sp.paid_at,
		sp.paid_through,sp.refunded_at,coalesce(sp.refund_status,'') FROM saas_payments sp
		JOIN saas_subscriptions s ON s.id=sp.saas_subscription_id JOIN workspaces w ON w.id=s.workspace_id
		ORDER BY sp.paid_at DESC LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, workspace, name, refundStatus string
		var payer int64
		var stars int
		var paidAt, through time.Time
		var refundedAt *time.Time
		if rows.Scan(&id, &workspace, &name, &payer, &stars, &paidAt, &through, &refundedAt, &refundStatus) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "workspace_id": workspace, "workspace_name": name, "payer_telegram_user_id": payer,
			"price_stars": stars, "paid_at": paidAt, "paid_through": through, "refunded_at": refundedAt, "refund_status": refundStatus})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"payments": items})
}
