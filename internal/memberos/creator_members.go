package memberos

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type memberRow struct {
	UserID         int64      `json:"telegram_user_id"`
	FirstName      string     `json:"first_name"`
	Username       string     `json:"username"`
	Access         *string    `json:"access_status"`
	Observed       *time.Time `json:"observed_at"`
	SubscriptionID *string    `json:"subscription_id"`
	Billing        *string    `json:"billing_status"`
	PaidThrough    *time.Time `json:"paid_through"`
	SubscribedAt   *time.Time `json:"subscribed_at"`
	Plan           *string    `json:"plan_name"`
	PlanID         *string    `json:"plan_id"`
	Entitled       bool       `json:"entitled"`
}

func (a *app) queryCreatorMembers(ctx context.Context, communityID, search, status, planID string, limit, offset int) ([]memberRow, error) {
	rows, err := a.db.Query(ctx, `SELECT m.telegram_user_id,m.first_name,m.username,ao.member_status,ao.observed_at,
        s.id,s.state,s.paid_through,s.first_paid_at,p.name,p.id,
        EXISTS(SELECT 1 FROM entitlements e WHERE e.community_id=c.id AND e.telegram_user_id=m.telegram_user_id
			AND e.status='active' AND e.valid_until>now())
        OR EXISTS(SELECT 1 FROM access_overrides o WHERE o.community_id=c.id
            AND o.telegram_user_id=m.telegram_user_id AND o.active=true AND o.valid_until>now())
        FROM communities c JOIN members m ON m.creator_id=c.creator_id
        LEFT JOIN access_observations ao ON ao.community_id=c.id AND ao.telegram_user_id=m.telegram_user_id
        LEFT JOIN LATERAL (SELECT id,state,paid_through,first_paid_at,plan_version_id FROM subscriptions
            WHERE community_id=c.id AND telegram_user_id=m.telegram_user_id
            ORDER BY last_payment_at DESC,id DESC LIMIT 1) s ON true
        LEFT JOIN plan_versions pv ON pv.id=s.plan_version_id
        LEFT JOIN plans p ON p.id=pv.plan_id
        WHERE c.id=$1 AND (ao.telegram_user_id IS NOT NULL OR s.id IS NOT NULL)
          AND ($2='' OR m.first_name ILIKE '%'||$2||'%' OR m.username ILIKE '%'||$2||'%'
               OR m.telegram_user_id::text LIKE '%'||$2||'%')
          AND ($3='' OR s.state=$3)
          AND ($4='' OR p.id::text=$4)
        ORDER BY m.verified_at DESC,m.telegram_user_id DESC LIMIT $5 OFFSET $6`, communityID, search, status, planID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]memberRow, 0)
	for rows.Next() {
		var item memberRow
		if err := rows.Scan(&item.UserID, &item.FirstName, &item.Username, &item.Access, &item.Observed,
			&item.SubscriptionID, &item.Billing, &item.PaidThrough, &item.SubscribedAt, &item.Plan, &item.PlanID, &item.Entitled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func memberFilters(r *http.Request) (search, status, planID string, offset int) {
	search = strings.TrimSpace(r.URL.Query().Get("q"))
	if len(search) > 80 {
		search = search[:80]
	}
	status = r.URL.Query().Get("status")
	switch status {
	case "active", "cancel_scheduled", "payment_failed", "expired", "refunded":
	default:
		status = ""
	}
	planID = r.URL.Query().Get("plan_id")
	if len(planID) > 40 {
		planID = ""
	}
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 10000 {
		offset = 0
	}
	return
}

func (a *app) creatorMembers(w http.ResponseWriter, r *http.Request) {
	access, ok := a.requireCreatorCommunity(w, r)
	if !ok {
		return
	}
	q, status, planID, offset := memberFilters(r)
	items, err := a.queryCreatorMembers(r.Context(), access.ID, q, status, planID, 51, offset)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	hasMore := len(items) > 50
	if hasMore {
		items = items[:50]
	}
	if access.Mode == "native_channel" {
		for i := range items {
			items[i].Billing = nil
			items[i].PaidThrough = nil
			items[i].SubscriptionID = nil
			items[i].Plan = nil
			items[i].PlanID = nil
		}
	}
	writeJSON(w, 200, map[string]any{"community_id": access.ID, "members": items, "limit": 50, "offset": offset, "has_more": hasMore, "coverage": "known members only"})
}

func (a *app) creatorMembersCSV(w http.ResponseWriter, r *http.Request) {
	access, ok := a.requireCreatorCommunity(w, r)
	if !ok {
		return
	}
	q, status, planID, _ := memberFilters(r)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=memberos-members.csv")
	w.Header().Set("Cache-Control", "no-store")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"telegram_user_id", "first_name", "username", "plan", "billing_status", "paid_through", "access_status", "observed_at"})
	for offset := 0; offset < 10000; offset += 500 {
		items, err := a.queryCreatorMembers(r.Context(), access.ID, q, status, planID, 500, offset)
		if err != nil {
			return
		}
		for _, m := range items {
			plan, billing, through := "", "", ""
			if access.Mode == "managed_bot" {
				if m.Plan != nil {
					plan = *m.Plan
				}
				if m.Billing != nil {
					billing = *m.Billing
				}
				if m.PaidThrough != nil {
					through = m.PaidThrough.UTC().Format(time.RFC3339)
				}
			}
			status, observed := "", ""
			if m.Access != nil {
				status = *m.Access
			}
			if m.Observed != nil {
				observed = m.Observed.UTC().Format(time.RFC3339)
			}
			_ = writer.Write([]string{strconv.FormatInt(m.UserID, 10), csvSafe(m.FirstName), csvSafe(m.Username), csvSafe(plan), billing, through, status, observed})
		}
		if len(items) < 500 {
			break
		}
	}
	writer.Flush()
}

func csvSafe(value string) string {
	if value != "" && strings.ContainsAny(value[:1], "=+-@\t\r\n") {
		return "'" + value
	}
	return value
}
