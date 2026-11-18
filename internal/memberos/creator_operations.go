package memberos

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) creatorManagedCommunity(w http.ResponseWriter, r *http.Request) (string, bool) {
	return a.creatorCommunityForMode(w, r, true)
}

func (a *app) creatorCommunityForMode(w http.ResponseWriter, r *http.Request, managedOnly bool) (string, bool) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	var mode string
	err = a.db.QueryRow(r.Context(), `SELECT c.billing_mode FROM communities c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id
        WHERE c.id=$1 AND wm.telegram_user_id=$2`, r.PathValue("id"), user).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return "", false
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return "", false
	}
	if managedOnly && mode != "managed_bot" {
		http.Error(w, "managed billing only", http.StatusUnprocessableEntity)
		return "", false
	}
	return r.PathValue("id"), true
}

func (a *app) creatorAccessMismatches(w http.ResponseWriter, r *http.Request) {
	community, ok := a.creatorManagedCommunity(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `WITH known AS (
          SELECT telegram_user_id FROM entitlements WHERE community_id=$1
          UNION SELECT telegram_user_id FROM access_observations WHERE community_id=$1
		  UNION SELECT telegram_user_id FROM access_overrides WHERE community_id=$1
        ) SELECT k.telegram_user_id,
	          member_has_access($1,k.telegram_user_id),
          o.member_status,o.observed_at,
          EXISTS(SELECT 1 FROM access_jobs j WHERE j.community_id=$1 AND j.telegram_user_id=k.telegram_user_id
            AND j.status='failed')
        FROM known k LEFT JOIN access_observations o ON o.community_id=$1 AND o.telegram_user_id=k.telegram_user_id
        ORDER BY k.telegram_user_id LIMIT 500`, community)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var user int64
		var expected, failed bool
		var status *string
		var at *time.Time
		if rows.Scan(&user, &expected, &status, &at, &failed) != nil {
			http.Error(w, "database error", 500)
			return
		}
		observed := status != nil && (*status == "member" || *status == "restricted" || *status == "administrator" || *status == "creator")
		stale := at == nil || at.Before(time.Now().Add(-24*time.Hour))
		if expected != observed || failed || stale {
			items = append(items, map[string]any{"telegram_user_id": user, "entitled": expected, "observed_status": status, "observed_at": at, "observation_stale": stale, "failed_job": failed})
		}
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"community_id": community, "mismatches": items, "coverage": "known members only; observations may be stale"})
}

func (a *app) creatorPayments(w http.ResponseWriter, r *http.Request) {
	community, ok := a.creatorManagedCommunity(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT p.id,s.telegram_user_id,p.telegram_charge_id,p.price_stars,p.paid_at,
	        p.provider_expiration,p.is_first_recurring,p.refunded_at,p.subscription_id,
	        CASE WHEN t.transaction_id IS NULL THEN 'not_observed'
	             WHEN t.amount_stars=p.price_stars AND t.raw->'source'->>'invoice_payload'=p.invoice_payload
	                  AND (t.raw->'source'->'user'->>'id')::bigint=s.telegram_user_id THEN 'matched'
	             ELSE 'discrepancy' END
        FROM payments p JOIN subscriptions s ON s.id=p.subscription_id
        LEFT JOIN observed_star_transactions t ON t.bot_id=p.billing_bot_id
            AND t.transaction_id=p.telegram_charge_id AND t.direction='incoming'
        WHERE s.community_id=$1
        ORDER BY p.paid_at DESC LIMIT 100`, community)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, charge, sub, reconciliation string
		var user int64
		var amount int
		var at, until time.Time
		var first bool
		var refunded *time.Time
		if rows.Scan(&id, &user, &charge, &amount, &at, &until, &first, &refunded, &sub, &reconciliation) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "telegram_user_id": user, "telegram_charge_id": charge, "amount_stars": amount, "paid_at": at, "paid_through": until, "first_payment": first, "refunded_at": refunded, "subscription_id": sub, "star_reconciliation": reconciliation})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"payments": items, "currency": "XTR", "coverage": "verified bot payments only"})
}

func (a *app) creatorSupportTickets(w http.ResponseWriter, r *http.Request) {
	community, ok := a.creatorCommunityForMode(w, r, false)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,telegram_user_id,kind,description,status,resolution,created_at
        FROM support_tickets WHERE community_id=$1 ORDER BY created_at DESC LIMIT 100`, community)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, kind, description, status string
		var user int64
		var resolution *string
		var at time.Time
		if rows.Scan(&id, &user, &kind, &description, &status, &resolution, &at) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "telegram_user_id": user, "kind": kind, "description": description, "status": status, "resolution": resolution, "created_at": at})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"tickets": items})
}

func (a *app) creatorResolveTicket(w http.ResponseWriter, r *http.Request) {
	community, ok := a.creatorCommunityForMode(w, r, false)
	if !ok {
		return
	}
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input struct {
		Resolution string `json:"resolution"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil || len(strings.TrimSpace(input.Resolution)) < 10 || len(input.Resolution) > 1000 {
		http.Error(w, "invalid resolution", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var member int64
	err = tx.QueryRow(r.Context(), `UPDATE support_tickets SET status='resolved',resolution=$3,updated_at=now()
        WHERE id=$1 AND community_id=$2 AND status<>'resolved' RETURNING telegram_user_id`, r.PathValue("ticket_id"), community, strings.TrimSpace(input.Resolution)).Scan(&member)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
        VALUES($1,'support_ticket_resolved',jsonb_build_object('ticket_id',$2::uuid,'member_id',$3::bigint))`, user, r.PathValue("ticket_id"), member)
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "resolved"})
}
