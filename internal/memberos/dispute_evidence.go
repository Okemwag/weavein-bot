package memberos

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (a *app) adminDisputeEvidence(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var evidence json.RawMessage
	err = tx.QueryRow(r.Context(), `SELECT jsonb_build_object(
		'ticket',jsonb_build_object('id',t.id,'community_id',t.community_id,'member_id',t.telegram_user_id,
			'kind',t.kind,'description',t.description,'status',t.status,'created_at',t.created_at,
			'resolution',t.resolution,'escalated_at',t.escalated_at,'escalation_reason',t.escalation_reason),
		'checkouts',coalesce((SELECT jsonb_agg(x) FROM (SELECT id,invoice_payload,status,price_stars,created_at,expires_at
			FROM checkout_attempts WHERE community_id=t.community_id AND telegram_user_id=t.telegram_user_id
			ORDER BY created_at DESC LIMIT 50) x),'[]'::jsonb),
		'subscriptions',coalesce((SELECT jsonb_agg(x) FROM (SELECT id,state,auto_renew_state,paid_through,
			first_paid_at,last_payment_at FROM subscriptions WHERE community_id=t.community_id
			AND telegram_user_id=t.telegram_user_id ORDER BY last_payment_at DESC LIMIT 50) x),'[]'::jsonb),
		'payments',coalesce((SELECT jsonb_agg(x) FROM (SELECT p.id,p.telegram_charge_id,p.price_stars,
			p.paid_at,p.provider_expiration,p.refunded_at FROM payments p JOIN subscriptions s ON s.id=p.subscription_id
			WHERE s.community_id=t.community_id AND s.telegram_user_id=t.telegram_user_id
			ORDER BY p.paid_at DESC LIMIT 100) x),'[]'::jsonb),
		'access',coalesce((SELECT jsonb_agg(x) FROM (SELECT member_status,observed_at,source
			FROM access_observations WHERE community_id=t.community_id AND telegram_user_id=t.telegram_user_id) x),'[]'::jsonb),
		'access_jobs',coalesce((SELECT jsonb_agg(x) FROM (SELECT id,action,status,attempts,last_error,finished_at
			FROM access_jobs WHERE community_id=t.community_id AND telegram_user_id=t.telegram_user_id
			ORDER BY run_after DESC LIMIT 100) x),'[]'::jsonb)
		) FROM support_tickets t WHERE t.id=$1`, r.PathValue("id")).Scan(&evidence)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,'dispute_evidence_viewed',jsonb_build_object('ticket_id',$2::uuid))`, operator, r.PathValue("id")); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"evidence": evidence, "coverage": "Known MemberOS records only; Telegram-native billing history is unavailable to this bot."})
}
