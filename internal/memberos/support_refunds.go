package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) createSupportTicket(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input struct {
		CommunityID string `json:"community_id"`
		Kind        string `json:"kind"`
		Description string `json:"description"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || len(strings.TrimSpace(input.Description)) < 10 || len(input.Description) > 2000 ||
		!validUUID(input.CommunityID) || (input.Kind != "access" && input.Kind != "payment" && input.Kind != "refund" && input.Kind != "other") {
		http.Error(w, "invalid ticket", 400)
		return
	}
	var id string
	err = a.db.QueryRow(r.Context(), `INSERT INTO support_tickets(community_id,telegram_user_id,kind,description)
        SELECT c.id,$2,$3,$4 FROM communities c WHERE c.id=$1 AND c.status='published'
        RETURNING id`, input.CommunityID, user, input.Kind, strings.TrimSpace(input.Description)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "community not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "status": "open"})
}

func (a *app) createRefundRequest(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input struct {
		PaymentID string `json:"payment_id"`
		Reason    string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil || !validUUID(input.PaymentID) || len(strings.TrimSpace(input.Reason)) < 10 || len(input.Reason) > 1000 {
		http.Error(w, "invalid request", 400)
		return
	}
	var id string
	err = a.db.QueryRow(r.Context(), `INSERT INTO refund_requests(payment_id,requested_by,reason)
        SELECT p.id,$2,$3 FROM payments p JOIN subscriptions s ON s.id=p.subscription_id
        JOIN communities c ON c.id=s.community_id
        WHERE p.id=$1 AND s.telegram_user_id=$2 AND p.refunded_at IS NULL AND c.billing_mode='managed_bot'
        RETURNING id`, input.PaymentID, user, strings.TrimSpace(input.Reason)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "You've already asked for a refund of this payment", http.StatusConflict)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "status": "requested"})
}

func (a *app) myPayments(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !validUUID(r.URL.Query().Get("community_id")) {
		http.Error(w, "invalid community", 400)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT p.id,p.price_stars,p.paid_at,p.refunded_at,
        EXISTS(SELECT 1 FROM refund_requests rr WHERE rr.payment_id=p.id AND rr.status IN ('requested','processing','submitted','needs_review'))
        FROM payments p JOIN subscriptions s ON s.id=p.subscription_id
        WHERE s.telegram_user_id=$1 AND s.community_id=$2 ORDER BY p.paid_at DESC LIMIT 50`, user, r.URL.Query().Get("community_id"))
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id string
		var amount int
		var paid time.Time
		var refunded *time.Time
		var requested bool
		if rows.Scan(&id, &amount, &paid, &refunded, &requested) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "amount_stars": amount, "paid_at": paid, "refunded_at": refunded, "refund_requested": requested})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"payments": items, "currency": "XTR"})
}

func (a *app) adminRefundRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT rr.id,rr.payment_id,rr.requested_by,rr.status,rr.reason,
        p.price_stars,p.telegram_charge_id,s.community_id,rr.created_at
        FROM refund_requests rr JOIN payments p ON p.id=rr.payment_id JOIN subscriptions s ON s.id=p.subscription_id
        WHERE rr.status IN ('requested','processing','submitted','needs_review') ORDER BY rr.created_at LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, payment, status, reason, charge, community string
		var requester int64
		var amount int
		var at time.Time
		if rows.Scan(&id, &payment, &requester, &status, &reason, &amount, &charge, &community, &at) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "payment_id": payment, "requested_by": requester, "status": status, "reason": reason, "amount_stars": amount, "charge_id": charge, "community_id": community, "created_at": at})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"refund_requests": items})
}

func (a *app) adminApproveRefund(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	// Claim the request before the external call. An uncertain result stays in needs_review;
	// it must never be silently retried because Telegram may have applied the refund.
	var user, bot int64
	var charge string
	err := a.db.QueryRow(r.Context(), `UPDATE refund_requests rr SET status='processing',decided_by=$2,updated_at=now()
        FROM payments p,subscriptions s WHERE rr.id=$1 AND rr.status='requested'
        AND p.id=rr.payment_id AND s.id=p.subscription_id AND p.refunded_at IS NULL
        RETURNING s.telegram_user_id,p.billing_bot_id,p.telegram_charge_id`, r.PathValue("id"), admin).Scan(&user, &bot, &charge)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "request unavailable", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if bot != a.cfg.botID {
		_, _ = a.db.Exec(r.Context(), `UPDATE refund_requests SET status='needs_review',decision_note='billing bot mismatch',updated_at=now() WHERE id=$1`, r.PathValue("id"))
		http.Error(w, "billing bot mismatch", http.StatusConflict)
		return
	}
	var accepted bool
	err = a.telegram.Call(r.Context(), "refundStarPayment", url.Values{"user_id": {strconv.FormatInt(user, 10)}, "telegram_payment_charge_id": {charge}}, &accepted)
	status := "submitted"
	if err != nil || !accepted {
		status = "needs_review"
	}
	_, dbErr := a.db.Exec(r.Context(), `UPDATE refund_requests SET status=$2,decision_note=$3,updated_at=now()
        WHERE id=$1 AND status='processing'`, r.PathValue("id"), status, map[bool]string{true: "Telegram accepted refund request; awaiting refund update", false: "Telegram outcome uncertain; review before further action"}[status == "submitted"])
	if dbErr != nil {
		http.Error(w, "We couldn't confirm the refund. It's now marked for review, so don't retry until you've checked", http.StatusServiceUnavailable)
		return
	}
	if _, err := a.db.Exec(context.WithoutCancel(r.Context()), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
        VALUES($1,'refund_approval',jsonb_build_object('request_id',$2::uuid,'outcome',$3::text))`, admin, r.PathValue("id"), status); err != nil {
		slog.Error("refund approval audit failed", "request_id", r.PathValue("id"), "outcome", status, "error", err)
	}
	if status != "submitted" {
		http.Error(w, "We couldn't confirm the refund. It's now marked for review, so don't retry until you've checked", http.StatusBadGateway)
		return
	}
	writeJSON(w, 200, map[string]string{"status": status})
}
