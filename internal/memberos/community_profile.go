package memberos

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) community(w http.ResponseWriter, r *http.Request) {
	var id, title, description, creator, support, mode, rules, guide, category string
	var nativeLink, managedLink *string
	var price int
	var verified *time.Time
	err := a.db.QueryRow(r.Context(), `SELECT c.id, c.title, c.description, cr.display_name, cr.support_contact,
		c.rules,c.content_guide,c.category,
		c.billing_mode,c.native_invite_link,c.managed_join_link,coalesce(offered.price_stars,c.price_stars),c.bot_admin_verified_at
        FROM communities c JOIN creators cr ON cr.id=c.creator_id
		LEFT JOIN LATERAL (SELECT min(v.price_stars) AS price_stars FROM plans p
			JOIN LATERAL (SELECT price_stars FROM plan_versions WHERE plan_id=p.id ORDER BY version DESC LIMIT 1) v ON true
			WHERE p.community_id=c.id AND p.status='active') offered ON true
		WHERE c.id=$1 AND c.status='published'`, r.PathValue("id")).Scan(&id, &title, &description, &creator, &support,
		&rules, &guide, &category, &mode, &nativeLink, &managedLink, &price, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	response := map[string]any{"id": id, "title": title, "description": description, "seller": creator, "support_contact": support,
		"rules": rules, "content_guide": guide, "category": category,
		"mode": mode, "price_stars": price, "period_seconds": 2592000, "bot_admin_verified_at": verified,
		"policies": a.cfg.policies}
	if mode == "native_channel" {
		response["subscription_link"] = nativeLink
	}
	if mode == "managed_bot" {
		response["checkout_path"] = "/api/v1/checkouts"
	}
	writeJSON(w, 200, response)
}
