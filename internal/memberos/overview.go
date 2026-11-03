package memberos

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) overview(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var id, mode string
	err = a.db.QueryRow(r.Context(), `SELECT c.id,c.billing_mode FROM communities c JOIN creator_users cu ON cu.creator_id=c.creator_id
        WHERE c.id::text=$1 AND cu.telegram_user_id=$2`, r.PathValue("id"), userID).Scan(&id, &mode)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var observed, knownExpiry int
	var lastObserved *time.Time
	err = a.db.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE member_status IN ('member','administrator','creator')),
		count(*) FILTER (WHERE member_status IN ('member','administrator','creator') AND subscription_until IS NOT NULL), max(observed_at)
        FROM access_observations WHERE community_id::text=$1`, id).Scan(&observed, &knownExpiry, &lastObserved)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	response := map[string]any{"community_id": id, "observed_members": observed, "known_expiry_count": knownExpiry,
		"last_observed_at": lastObserved, "coverage": "known members only", "billing_metrics": "unavailable"}
	if mode == "managed_bot" {
		var active, failed int
		var gross, refunded int64
		err = a.db.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE state IN ('active','cancel_scheduled','payment_failed') AND paid_through>now()),
            count(*) FILTER (WHERE state='payment_failed') FROM subscriptions WHERE community_id=$1`, id).Scan(&active, &failed)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		err = a.db.QueryRow(r.Context(), `SELECT coalesce(sum(p.price_stars),0),coalesce(sum(p.price_stars) FILTER (WHERE p.refunded_at IS NOT NULL),0)
            FROM payments p JOIN subscriptions s ON s.id=p.subscription_id WHERE s.community_id=$1`, id).Scan(&gross, &refunded)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		response["billing_metrics"] = map[string]any{"active_paid_subscriptions": active, "failed_renewals": failed,
			"recorded_gross_stars": gross, "recorded_refunded_stars": refunded, "currency": "XTR", "coverage": "bot payment updates recorded by MemberOS"}
	}
	writeJSON(w, 200, response)
}
