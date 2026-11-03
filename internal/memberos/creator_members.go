package memberos

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) creatorMembers(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	communityID := r.PathValue("id")
	var mode string
	err = a.db.QueryRow(r.Context(), `SELECT c.billing_mode FROM communities c JOIN creator_users cu ON cu.creator_id=c.creator_id
        WHERE c.id::text=$1 AND cu.telegram_user_id=$2`, communityID, userID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT m.telegram_user_id,m.first_name,ao.member_status,ao.observed_at,
        s.state,s.paid_through, EXISTS(SELECT 1 FROM entitlements e WHERE e.community_id=c.id
        AND e.telegram_user_id=m.telegram_user_id AND e.status='active' AND e.valid_until>now())
        FROM communities c JOIN members m ON m.creator_id=c.creator_id
        LEFT JOIN access_observations ao ON ao.community_id=c.id AND ao.telegram_user_id=m.telegram_user_id
        LEFT JOIN LATERAL (SELECT state,paid_through FROM subscriptions WHERE community_id=c.id
            AND telegram_user_id=m.telegram_user_id ORDER BY last_payment_at DESC LIMIT 1) s ON true
        WHERE c.id::text=$1 AND (ao.telegram_user_id IS NOT NULL OR s.state IS NOT NULL)
        ORDER BY m.verified_at DESC LIMIT 50`, communityID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var name string
		var access, billing *string
		var observed, paidThrough *time.Time
		var entitled bool
		if err := rows.Scan(&id, &name, &access, &observed, &billing, &paidThrough, &entitled); err != nil {
			http.Error(w, "database error", 500)
			return
		}
		item := map[string]any{"telegram_user_id": id, "first_name": name, "access_status": access, "observed_at": observed}
		if mode == "managed_bot" {
			item["billing_status"] = billing
			item["paid_through"] = paidThrough
			item["entitled"] = entitled
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"community_id": communityID, "members": result, "limit": 50, "coverage": "known members only"})
}
