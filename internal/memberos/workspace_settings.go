package memberos

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

func (a *app) updateWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input struct {
		Timezone             string `json:"timezone"`
		Language             string `json:"language"`
		NotificationsEnabled *bool  `json:"notifications_enabled"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil || input.NotificationsEnabled == nil {
		http.Error(w, "invalid settings", http.StatusBadRequest)
		return
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil || len(input.Timezone) > 80 || input.Timezone == "" {
		http.Error(w, "invalid timezone", http.StatusBadRequest)
		return
	}
	if input.Language != "en" && input.Language != "sw" {
		http.Error(w, "unsupported language", http.StatusBadRequest)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var creator string
	err = tx.QueryRow(r.Context(), `UPDATE workspaces w SET timezone=$3,language=$4,notifications_enabled=$5
 FROM workspace_members wm WHERE w.id=$1 AND wm.workspace_id=w.id AND wm.telegram_user_id=$2 AND wm.role='owner'
 RETURNING w.creator_id`, r.PathValue("id"), user, input.Timezone, input.Language, *input.NotificationsEnabled).Scan(&creator)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
 VALUES($1,$2,'workspace_settings_updated',jsonb_build_object('workspace_id',$3::uuid))`, creator, user, r.PathValue("id"))
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"timezone": input.Timezone, "language": input.Language, "notifications_enabled": *input.NotificationsEnabled})
}

func (a *app) requestWorkspaceDeletion(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
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
	var creator string
	err = tx.QueryRow(r.Context(), `UPDATE workspaces w SET deletion_requested_at=coalesce(deletion_requested_at,now()),
 deletion_requested_by=coalesce(deletion_requested_by,$2)
 FROM workspace_members wm WHERE w.id=$1 AND wm.workspace_id=w.id AND wm.telegram_user_id=$2 AND wm.role='owner'
 RETURNING w.creator_id`, r.PathValue("id"), user).Scan(&creator)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
 VALUES($1,$2,'workspace_deletion_requested',jsonb_build_object('workspace_id',$3::uuid))`, creator, user, r.PathValue("id"))
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "requested", "next_step": "MemberOS support will review active memberships and retention obligations before deletion."})
}
