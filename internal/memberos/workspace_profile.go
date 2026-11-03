package memberos

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (a *app) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Name           string `json:"name"`
		SupportContact string `json:"support_contact"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.SupportContact = strings.TrimSpace(body.SupportContact)
	if len(body.Name) < 1 || len(body.Name) > 120 || len(body.SupportContact) < 3 || len(body.SupportContact) > 240 {
		http.Error(w, "name or support_contact invalid", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var workspaceID, creatorID string
	err = tx.QueryRow(r.Context(), `SELECT w.id,w.creator_id FROM workspaces w
		JOIN workspace_members wm ON wm.workspace_id=w.id AND wm.telegram_user_id=$2 AND wm.role='owner'
		WHERE w.id::text=$1 FOR UPDATE OF w`, r.PathValue("id"), userID).Scan(&workspaceID, &creatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE workspaces SET name=$2 WHERE id=$1`, workspaceID, body.Name); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE creators SET display_name=$2,support_contact=$3 WHERE id=$1`, creatorID, body.Name, body.SupportContact); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
		VALUES($1,$2,'workspace_profile_updated',jsonb_build_object('workspace_id',$3::text))`, creatorID, userID, workspaceID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"id": workspaceID, "name": body.Name, "support_contact": body.SupportContact})
}
