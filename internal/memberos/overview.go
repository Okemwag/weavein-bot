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
	period, err := parseReportPeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		"period_from": period.From, "period_to_exclusive": period.To,
		"last_observed_at": lastObserved, "coverage": "known members only", "billing_metrics": "unavailable"}
	if mode == "managed_bot" {
		var active, failed, newSubscribers int
		var gross, refunded int64
		err = a.db.QueryRow(r.Context(), `SELECT count(DISTINCT telegram_user_id) FILTER
            (WHERE state IN ('active','cancel_scheduled','payment_failed') AND paid_through>now()),
            count(*) FILTER (WHERE state='payment_failed'),
            count(DISTINCT telegram_user_id) FILTER (WHERE first_paid_at >= $2 AND first_paid_at < $3)
            FROM subscriptions WHERE community_id=$1`, id, period.From, period.To).Scan(&active, &failed, &newSubscribers)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		err = a.db.QueryRow(r.Context(), `SELECT coalesce(sum(p.price_stars),0),coalesce(sum(p.price_stars) FILTER (WHERE p.refunded_at IS NOT NULL),0)
            FROM payments p JOIN subscriptions s ON s.id=p.subscription_id WHERE s.community_id=$1
            AND p.paid_at >= $2 AND p.paid_at < $3`, id, period.From, period.To).Scan(&gross, &refunded)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		var canceled, expired int
		err = a.db.QueryRow(r.Context(), `SELECT count(DISTINCT e.subscription_id) FILTER
            (WHERE e.kind IN ('subscription_canceled','cancellation_requested')),
            count(DISTINCT e.subscription_id) FILTER (WHERE e.kind='entitlement_expired')
            FROM subscription_events e JOIN subscriptions s ON s.id=e.subscription_id
            WHERE s.community_id=$1 AND e.occurred_at>=$2 AND e.occurred_at<$3`, id, period.From, period.To).Scan(&canceled, &expired)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		var renewed int
		err = a.db.QueryRow(r.Context(), `SELECT count(*) FROM payments p JOIN subscriptions s ON s.id=p.subscription_id
            WHERE s.community_id=$1 AND p.is_first_recurring=false AND p.paid_at>=$2 AND p.paid_at<$3`, id, period.From, period.To).Scan(&renewed)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		var failedAttempts int
		err = a.db.QueryRow(r.Context(), `SELECT count(*) FROM subscription_events e JOIN subscriptions s ON s.id=e.subscription_id
            WHERE s.community_id=$1 AND e.kind='subscription_failed' AND e.occurred_at>=$2 AND e.occurred_at<$3`, id, period.From, period.To).Scan(&failedAttempts)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		var awaiting, failedJobs int
		err = a.db.QueryRow(r.Context(), `SELECT count(DISTINCT e.telegram_user_id) FROM entitlements e
            LEFT JOIN access_observations o ON o.community_id=e.community_id AND o.telegram_user_id=e.telegram_user_id
            WHERE e.community_id=$1 AND e.status='active' AND e.valid_until>now()
            AND (o.member_status IS NULL OR o.member_status NOT IN ('member','administrator','creator','restricted'))`, id).Scan(&awaiting)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		err = a.db.QueryRow(r.Context(), `SELECT count(*) FROM access_jobs WHERE community_id=$1 AND status='failed'`, id).Scan(&failedJobs)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		rows, err := a.db.Query(r.Context(), `SELECT coalesce(p.name,'Legacy plan'),count(DISTINCT s.telegram_user_id)
            FROM subscriptions s LEFT JOIN plan_versions pv ON pv.id=s.plan_version_id
            LEFT JOIN plans p ON p.id=pv.plan_id WHERE s.community_id=$1 AND s.paid_through>now()
            AND s.state IN ('active','cancel_scheduled','payment_failed')
            GROUP BY p.name ORDER BY p.name`, id)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		planCounts := make([]map[string]any, 0)
		for rows.Next() {
			var name string
			var count int
			if rows.Scan(&name, &count) != nil {
				rows.Close()
				http.Error(w, "database error", 500)
				return
			}
			planCounts = append(planCounts, map[string]any{"plan": name, "subscribers": count})
		}
		if rows.Err() != nil {
			rows.Close()
			http.Error(w, "database error", 500)
			return
		}
		rows.Close()
		response["billing_metrics"] = map[string]any{"active_paid_subscriptions": active, "failed_renewals": failed,
			"new_subscribers": newSubscribers, "cancellations": canceled, "expired_subscriptions": expired,
			"observed_renewal_successes": renewed, "observed_renewal_failures": failedAttempts,
			"subscribers_awaiting_access": awaiting, "failed_access_jobs": failedJobs, "paid_by_plan": planCounts,
			"recorded_gross_stars": gross, "recorded_refunded_stars": refunded, "currency": "XTR",
			"coverage": "verified bot events; renewal attempts and older native channel billing may be incomplete"}
	}
	writeJSON(w, 200, response)
}
