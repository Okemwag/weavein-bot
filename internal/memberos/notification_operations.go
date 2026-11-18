package memberos

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) adminNotifications(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,telegram_user_id,dedupe_key,status,attempts,last_error,last_attempt_at
		FROM notifications WHERE status IN ('failed','uncertain')
		OR (status='sending' AND last_attempt_at<now()-interval '2 minutes')
		ORDER BY last_attempt_at DESC NULLS LAST LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, key, status string
		var user int64
		var attempts int
		var reason *string
		var at *time.Time
		if err := rows.Scan(&id, &user, &key, &status, &attempts, &reason, &at); err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		items = append(items, map[string]any{"id": id, "telegram_user_id": user, "dedupe_key": key,
			"status": status, "attempts": attempts, "error": reason, "last_attempt_at": at})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": items})
}

func (a *app) adminRetryNotification(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var status, key string
	var user int64
	var attemptedAt *time.Time
	err = tx.QueryRow(r.Context(), `SELECT status,dedupe_key,telegram_user_id,last_attempt_at
		FROM notifications WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&status, &key, &user, &attemptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	acknowledged := r.URL.Query().Get("acknowledge_duplicate_risk") == "true"
	if status == "sending" && (attemptedAt == nil || time.Since(*attemptedAt) < 2*time.Minute) {
		http.Error(w, "delivery attempt is still in progress", http.StatusConflict)
		return
	}
	if status != "failed" && status != "uncertain" && status != "sending" {
		http.Error(w, "notification is not retryable", http.StatusConflict)
		return
	}
	if status != "failed" && !acknowledged {
		http.Error(w, "acknowledge possible duplicate delivery before retrying", http.StatusBadRequest)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE notifications SET status='pending',attempts=0,
		run_after=now(),last_error=NULL,last_attempt_at=NULL WHERE id=$1`, r.PathValue("id")); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,'notification_retry',jsonb_build_object('notification_id',$2::uuid,'dedupe_key',$3::text,
		'target_user',$4::bigint,'previous_status',$5::text,'duplicate_risk_acknowledged',$6::boolean))`,
		admin, r.PathValue("id"), key, user, status, acknowledged); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}
