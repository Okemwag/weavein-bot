package memberos

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (a *app) workspaces(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		rows, err := a.db.Query(r.Context(), `SELECT w.id,w.name,c.display_name,c.support_contact,wm.role
			FROM workspaces w JOIN creators c ON c.id=w.creator_id JOIN workspace_members wm ON wm.workspace_id=w.id
			WHERE wm.telegram_user_id=$1 ORDER BY w.created_at`, userID)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		defer rows.Close()
		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, seller, support, role string
			if err := rows.Scan(&id, &name, &seller, &support, &role); err != nil {
				http.Error(w, "database error", 500)
				return
			}
			managedAllowed := a.cfg.managedEnabled && a.cfg.botRecipientAck == strconv.FormatInt(a.cfg.botID, 10) && a.cfg.managedWorkspaceID == id
			result = append(result, map[string]any{"id": id, "name": name, "seller": seller, "support_contact": support, "role": role, "managed_billing_enabled": managedAllowed})
		}
		if rows.Err() != nil {
			http.Error(w, "database error", 500)
			return
		}
		writeJSON(w, 200, map[string]any{"workspaces": result})
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
	// The verified session is the only source of the owner ID.
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var creatorID, workspaceID string
	if err := tx.QueryRow(r.Context(), `INSERT INTO creators(display_name,support_contact) VALUES($1,$2) RETURNING id`, body.Name, body.SupportContact).Scan(&creatorID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.QueryRow(r.Context(), `INSERT INTO workspaces(creator_id,name) VALUES($1,$2) RETURNING id`, creatorID, body.Name).Scan(&workspaceID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO creator_users(creator_id,telegram_user_id,role) VALUES($1,$2,'owner')`, creatorID, userID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO workspace_members(workspace_id,telegram_user_id,role) VALUES($1,$2,'owner')`, workspaceID, userID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 201, map[string]any{"id": workspaceID, "creator_id": creatorID, "name": body.Name})
}
