package memberos

import (
	"net/http"
	"time"
)

func (a *app) creatorCommunities(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT c.id,c.workspace_id,c.title,c.billing_mode,c.status,
        c.connection_status,c.last_permission_check_at,c.last_permission_error FROM communities c
        JOIN creator_users cu ON cu.creator_id=c.creator_id
        WHERE cu.telegram_user_id=$1 ORDER BY c.created_at DESC LIMIT 100`, userID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, workspaceID, title, mode, status, connection string
		var checked *time.Time
		var permissionError *string
		if err := rows.Scan(&id, &workspaceID, &title, &mode, &status, &connection, &checked, &permissionError); err != nil {
			http.Error(w, "database error", 500)
			return
		}
		result = append(result, map[string]any{"id": id, "workspace_id": workspaceID, "title": title, "mode": mode, "status": status,
			"connection_status": connection, "last_permission_check_at": checked, "last_permission_error": permissionError})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"communities": result})
}
