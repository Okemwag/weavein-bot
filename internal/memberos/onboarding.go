package memberos

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (a *app) onboarding(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var workspaceID, name, support string
	err = a.db.QueryRow(r.Context(), `SELECT w.id,w.name,c.support_contact FROM workspaces w
		JOIN creators c ON c.id=w.creator_id JOIN workspace_members wm ON wm.workspace_id=w.id AND wm.telegram_user_id=$2
		WHERE w.id::text=$1`, r.PathValue("id"), userID).Scan(&workspaceID, &name, &support)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var connected, published, offersReady int
	err = a.db.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER (WHERE status='published'),
		count(*) FILTER (WHERE status='published' AND EXISTS(
			SELECT 1 FROM plans p JOIN plan_versions v ON v.plan_id=p.id
			WHERE p.community_id=communities.id AND p.status='active'))
		FROM communities WHERE workspace_id=$1`, workspaceID).Scan(&connected, &published, &offersReady)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	steps := []map[string]any{
		{"key": "workspace", "label": "Create workspace", "complete": true},
		{"key": "profile", "label": "Set creator name and support contact", "complete": name != "" && support != ""},
		{"key": "connection", "label": "Connect a private Telegram community", "complete": connected > 0},
		{"key": "published", "label": "Verify bot rights and publish", "complete": published > 0},
		{"key": "offer", "label": "Make a membership plan available", "complete": offersReady > 0},
	}
	writeJSON(w, 200, map[string]any{"workspace_id": workspaceID, "setup_complete": offersReady > 0, "connected_communities": connected,
		"published_communities": published, "communities_with_active_offers": offersReady, "steps": steps})
}
