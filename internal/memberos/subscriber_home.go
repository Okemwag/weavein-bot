package memberos

import (
	"net/http"
	"time"
)

func (a *app) myMemberships(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := a.db.Query(r.Context(), `WITH known AS (
            SELECT community_id FROM subscriptions WHERE telegram_user_id=$1
            UNION SELECT community_id FROM access_observations WHERE telegram_user_id=$1
		) SELECT c.id,c.title,c.billing_mode,c.status,s.state,s.paid_through,s.auto_renew_state,
            ao.member_status,ao.observed_at,
            member_has_access(c.id,$1),
            (SELECT count(*) FROM subscriptions l WHERE l.community_id=c.id AND l.telegram_user_id=$1
                AND l.state NOT IN ('expired','refunded') AND l.auto_renew_state='on')
        FROM known k JOIN communities c ON c.id=k.community_id
        LEFT JOIN LATERAL (SELECT state,paid_through,auto_renew_state FROM subscriptions
            WHERE community_id=c.id AND telegram_user_id=$1 ORDER BY last_payment_at DESC LIMIT 1) s ON true
        LEFT JOIN access_observations ao ON ao.community_id=c.id AND ao.telegram_user_id=$1
        ORDER BY c.title LIMIT 100`, user)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, title, mode, communityStatus string
		var state, renew, access *string
		var until, observed *time.Time
		var entitled bool
		var renewing int
		if rows.Scan(&id, &title, &mode, &communityStatus, &state, &until, &renew, &access, &observed, &entitled, &renewing) != nil {
			http.Error(w, "database error", 500)
			return
		}
		item := map[string]any{"community_id": id, "title": title, "mode": mode, "community_status": communityStatus, "access_status": access, "observed_at": observed}
		if mode == "managed_bot" {
			item["billing_status"] = state
			item["paid_through"] = until
			item["auto_renew_state"] = renew
			item["entitled"] = entitled
			item["renewing_subscriptions"] = renewing
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"memberships": items, "coverage": "known managed subscriptions and observed native memberships"})
}
