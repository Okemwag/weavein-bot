package memberos

import (
	"net/http"
)

func (a *app) listCommunities(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `SELECT c.id,c.title,c.description,coalesce(offered.price_stars,c.price_stars),c.billing_mode,cr.display_name
        FROM communities c JOIN creators cr ON cr.id=c.creator_id
		LEFT JOIN LATERAL (SELECT min(v.price_stars) AS price_stars FROM plans p
			JOIN LATERAL (SELECT price_stars FROM plan_versions WHERE plan_id=p.id ORDER BY version DESC LIMIT 1) v ON true
			WHERE p.community_id=c.id AND p.status='active') offered ON true
        WHERE c.status='published' ORDER BY c.created_at DESC LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, title, description, mode, seller string
		var price int
		if err := rows.Scan(&id, &title, &description, &price, &mode, &seller); err != nil {
			http.Error(w, "database error", 500)
			return
		}
		result = append(result, map[string]any{"id": id, "title": title, "description": description, "price_stars": price, "mode": mode, "seller": seller})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"communities": result})
}
