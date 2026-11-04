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
	err = a.db.QueryRow(r.Context(), `SELECT c.id,c.billing_mode,cu.role FROM communities c
        JOIN creator_users cu ON cu.creator_id=c.creator_id
        WHERE c.id::text=$1 AND cu.telegram_user_id=$2`, r.PathValue("id"), user).
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
