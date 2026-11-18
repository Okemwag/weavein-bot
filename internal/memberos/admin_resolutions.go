package memberos

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// resolutionNote reads the operator's required explanation for an audited decision.
func resolutionNote(w http.ResponseWriter, r *http.Request) (string, bool) {
	var input struct {
		Note string `json:"note"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return "", false
	}
	input.Note = strings.TrimSpace(input.Note)
	if len(input.Note) < 10 || len(input.Note) > 1000 {
		http.Error(w, "a note of 10-1000 characters is required", http.StatusBadRequest)
		return "", false
	}
	return input.Note, true
}

// audited runs one state change and its audit record in a single transaction.
// change reports false when no row was eligible.
func (a *app) audited(ctx context.Context, w http.ResponseWriter, operator int64, action, ref, note string, change func(pgx.Tx) (bool, error)) bool {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return false
	}
	defer func() { _ = tx.Rollback(ctx) }()
	changed, err := change(tx)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return false
	}
	if !changed {
		http.Error(w, "item is not available for this action", http.StatusConflict)
		return false
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,$2,jsonb_build_object('ref',$3::text,'note',$4::text))`, operator, action, ref, note); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return false
	}
	return true
}

func rowChanged(tx pgx.Tx, ctx context.Context, sql string, args ...any) (bool, error) {
	tag, err := tx.Exec(ctx, sql, args...)
	return tag.RowsAffected() > 0, err
}

func (a *app) adminAcknowledgeUpdate(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	botID, e1 := strconv.ParseInt(r.PathValue("bot_id"), 10, 64)
	updateID, e2 := strconv.ParseInt(r.PathValue("update_id"), 10, 64)
	if e1 != nil || e2 != nil {
		http.Error(w, "invalid update", http.StatusBadRequest)
		return
	}
	note, ok := resolutionNote(w, r)
	if !ok {
		return
	}
	if a.audited(r.Context(), w, operator, "webhook_update_acknowledged", strconv.FormatInt(updateID, 10), note, func(tx pgx.Tx) (bool, error) {
		return rowChanged(tx, r.Context(), `UPDATE telegram_updates SET state='acknowledged'
			WHERE bot_id=$1 AND update_id=$2 AND state IN ('failed','quarantined','answer_unknown')`, botID, updateID)
	}) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
	}
}

func (a *app) adminAcknowledgeAccessJob(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	note, ok := resolutionNote(w, r)
	if !ok {
		return
	}
	if a.audited(r.Context(), w, operator, "access_job_acknowledged", r.PathValue("id"), note, func(tx pgx.Tx) (bool, error) {
		return rowChanged(tx, r.Context(), `UPDATE access_jobs SET status='acknowledged' WHERE id=$1 AND status='failed'`, r.PathValue("id"))
	}) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
	}
}

func (a *app) adminAcknowledgeNotification(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	note, ok := resolutionNote(w, r)
	if !ok {
		return
	}
	if a.audited(r.Context(), w, operator, "notification_acknowledged", r.PathValue("id"), note, func(tx pgx.Tx) (bool, error) {
		return rowChanged(tx, r.Context(), `UPDATE notifications SET status='acknowledged'
			WHERE id=$1 AND status IN ('failed','uncertain')`, r.PathValue("id"))
	}) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
	}
}

// adminRetryUncertainNotifications requeues every uncertain notice after an
// outage, so an operator does not have to retry them one at a time.
func (a *app) adminRetryUncertainNotifications(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("acknowledge_duplicate_risk") != "true" {
		http.Error(w, "acknowledge possible duplicate delivery before retrying", http.StatusBadRequest)
		return
	}
	var requeued int64
	if a.audited(r.Context(), w, operator, "notifications_bulk_retry", "uncertain", "operator accepted duplicate delivery risk", func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(r.Context(), `UPDATE notifications SET status='pending',attempts=0,run_after=now(),
			last_error=NULL,last_attempt_at=NULL WHERE status='uncertain'`)
		requeued = tag.RowsAffected()
		return true, err
	}) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "requeued": requeued})
	}
}

// adminRejectRefund closes a request without refunding. needs_review is included:
// the operator must first have checked Telegram for the earlier attempt's outcome.
func (a *app) adminRejectRefund(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	note, ok := resolutionNote(w, r)
	if !ok {
		return
	}
	if a.audited(r.Context(), w, operator, "refund_rejected", r.PathValue("id"), note, func(tx pgx.Tx) (bool, error) {
		return rowChanged(tx, r.Context(), `UPDATE refund_requests SET status='rejected',decided_by=$2,decision_note=$3,updated_at=now()
			WHERE id=$1 AND status IN ('requested','needs_review')`, r.PathValue("id"), operator, note)
	}) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
	}
}

// adminReopenRefund returns an uncertain request to the approval queue once the
// operator has confirmed that Telegram did not apply the earlier attempt.
func (a *app) adminReopenRefund(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	note, ok := resolutionNote(w, r)
	if !ok {
		return
	}
	if a.audited(r.Context(), w, operator, "refund_reopened", r.PathValue("id"), note, func(tx pgx.Tx) (bool, error) {
		return rowChanged(tx, r.Context(), `UPDATE refund_requests rr SET status='requested',decided_by=$2,decision_note=$3,updated_at=now()
			FROM payments p WHERE rr.id=$1 AND rr.status='needs_review' AND p.id=rr.payment_id AND p.refunded_at IS NULL`, r.PathValue("id"), operator, note)
	}) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "requested"})
	}
}
