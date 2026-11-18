package memberos

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type creatorCommunityAccess struct {
	ID   string
	Mode string
	Role string
	User int64
}

func (a *app) requireCreatorCommunity(w http.ResponseWriter, r *http.Request) (creatorCommunityAccess, bool) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return creatorCommunityAccess{}, false
	}
	var access creatorCommunityAccess
	access.User = user
	err = a.db.QueryRow(r.Context(), `SELECT c.id,c.billing_mode,wm.role FROM communities c
        JOIN workspace_members wm ON wm.workspace_id=c.workspace_id
        WHERE c.id=$1 AND wm.telegram_user_id=$2`, r.PathValue("id"), user).
		Scan(&access.ID, &access.Mode, &access.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return creatorCommunityAccess{}, false
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return creatorCommunityAccess{}, false
	}
	return access, true
}
