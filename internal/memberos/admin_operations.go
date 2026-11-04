package memberos

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) requireAdmin(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := a.sessionUser(r)
	if err != nil || a.cfg.adminTelegramID == 0 || id != a.cfg.adminTelegramID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return 0, false
	}
	return id, true
}

func (a *app) adminUpdates(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT bot_id,update_id,update_type,state,attempts,last_error,received_at
        FROM telegram_updates WHERE state IN ('failed','quarantined') ORDER BY received_at DESC LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var botID, updateID int64
		var kind, state string
		var attempts int
		var reason *string
		var at time.Time
		if rows.Scan(&botID, &updateID, &kind, &state, &attempts, &reason, &at) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"bot_id": botID, "update_id": updateID, "type": kind, "state": state, "attempts": attempts, "error": reason, "received_at": at})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	recentRows, err := a.db.Query(r.Context(), `SELECT bot_id,update_id,update_type,state,attempts,last_error,
		received_at,processed_at FROM telegram_updates ORDER BY received_at DESC LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer recentRows.Close()
	recent := make([]map[string]any, 0)
	for recentRows.Next() {
		var botID, updateID int64
		var kind, state string
		var attempts int
		var reason *string
		var received time.Time
		var processed *time.Time
		if recentRows.Scan(&botID, &updateID, &kind, &state, &attempts, &reason, &received, &processed) != nil {
			http.Error(w, "database error", 500)
			return
		}
		recent = append(recent, map[string]any{"bot_id": botID, "update_id": updateID, "type": kind,
			"state": state, "attempts": attempts, "error": reason, "received_at": received, "processed_at": processed})
	}
	if recentRows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"updates": items, "recent": recent, "coverage": "latest 100 stored Telegram updates; payloads withheld"})
}

func (a *app) adminRetryUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	botID, e1 := strconv.ParseInt(r.PathValue("bot_id"), 10, 64)
	updateID, e2 := strconv.ParseInt(r.PathValue("update_id"), 10, 64)
	if e1 != nil || e2 != nil || botID != a.cfg.botID || updateID <= 0 {
		http.Error(w, "invalid update", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var kind string
	err = tx.QueryRow(r.Context(), `UPDATE telegram_updates SET state='pending',attempts=0,last_error=NULL,processed_at=NULL,run_after=now()
		WHERE bot_id=$1 AND update_id=$2 AND state IN ('failed','quarantined') RETURNING update_type`, botID, updateID).Scan(&kind)
	if err == pgx.ErrNoRows {
		http.Error(w, "retryable update not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,update_id,details)
        VALUES($1,'webhook_retry',$2,jsonb_build_object('bot_id',$3,'kind',$4))`, id, updateID, botID, kind)
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "queued"})
}

func (a *app) adminAccessJobs(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,community_id,telegram_user_id,action,attempts,last_error,finished_at
        FROM access_jobs WHERE status='failed' ORDER BY finished_at DESC LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, community, action string
		var user int64
		var attempts int
		var reason *string
		var at *time.Time
		if rows.Scan(&id, &community, &user, &action, &attempts, &reason, &at) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "community_id": community, "telegram_user_id": user, "action": action, "attempts": attempts, "error": reason, "finished_at": at})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": items})
}

func (a *app) adminRetryAccessJob(w http.ResponseWriter, r *http.Request) {
	id, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var community string
	var user int64
	err = tx.QueryRow(r.Context(), `UPDATE access_jobs SET status='pending',attempts=0,last_error=NULL,finished_at=NULL,run_after=now()
        WHERE id::text=$1 AND status='failed' RETURNING community_id,telegram_user_id`, r.PathValue("id")).Scan(&community, &user)
	if err == pgx.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
        VALUES($1,'access_job_retry',jsonb_build_object('job_id',$2,'community_id',$3,'member_id',$4))`, id, r.PathValue("id"), community, user)
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "queued"})
}

func (a *app) adminReconcileAccess(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if err := a.accessService().ReconcileKnown(r.Context()); err != nil {
		http.Error(w, "reconciliation failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "complete", "coverage": "known managed-community members only"})
}
