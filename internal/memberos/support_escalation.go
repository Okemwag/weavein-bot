package memberos

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (a *app) adminEscalateSupportTicket(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Reason) < 10 || len(input.Reason) > 1000 {
		http.Error(w, "invalid escalation reason", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var community string
	var member int64
	err = tx.QueryRow(r.Context(), `UPDATE support_tickets SET status='in_progress',assigned_operator_id=$2,
		escalated_at=now(),escalation_reason=$3,updated_at=now()
		WHERE id=$1 AND status<>'resolved' AND escalated_at IS NULL
		RETURNING community_id,telegram_user_id`, r.PathValue("id"), operator, input.Reason).Scan(&community, &member)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "ticket unavailable for escalation", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,'platform_support_escalated',jsonb_build_object('ticket_id',$2::uuid,'community_id',$3::uuid,
		'member_id',$4::bigint,'reason',$5::text))`, operator, r.PathValue("id"), community, member, input.Reason)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "in_progress", "escalation": "recorded"})
}
