package memberos

import (
	"encoding/json"
	"errors"
	"log/slog"
	"memberos/internal/models"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (a *app) plans(w http.ResponseWriter, r *http.Request) {
	communityID := r.PathValue("id")
	if r.Method == http.MethodGet {
		rows, err := a.db.Query(r.Context(), `SELECT p.id,p.name,p.status,v.id,v.version,v.price_stars,v.description,v.benefits,v.terms
			FROM plans p JOIN LATERAL (SELECT * FROM plan_versions WHERE plan_id=p.id ORDER BY version DESC LIMIT 1) v ON true
			JOIN communities c ON c.id=p.community_id WHERE c.id::text=$1 AND c.status='published' AND p.status='active'
			ORDER BY p.created_at`, communityID)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		defer rows.Close()
		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, status, versionID, description, benefits, terms string
			var version, price int
			if err := rows.Scan(&id, &name, &status, &versionID, &version, &price, &description, &benefits, &terms); err != nil {
				http.Error(w, "database error", 500)
				return
			}
			result = append(result, map[string]any{"id": id, "name": name, "status": status, "version_id": versionID, "version": version, "price_stars": price, "period_seconds": models.SubscriptionPeriodSeconds, "description": description, "benefits": benefits, "terms": terms})
		}
		if rows.Err() != nil {
			http.Error(w, "database error", 500)
			return
		}
		writeJSON(w, 200, map[string]any{"plans": result})
		return
	}
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Name string `json:"name"`
		models.PlanTerms
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if len(body.Name) < 1 || len(body.Name) > 80 || body.Validate() != nil {
		http.Error(w, "invalid plan", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var workspaceID string
	err = tx.QueryRow(r.Context(), `SELECT c.workspace_id FROM communities c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id
		WHERE c.id::text=$1 AND wm.telegram_user_id=$2 AND wm.role IN ('owner','admin') AND c.billing_mode='managed_bot'`, communityID, userID).Scan(&workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var planID, versionID string
	err = tx.QueryRow(r.Context(), `INSERT INTO plans(workspace_id,community_id,name) VALUES($1,$2,$3) RETURNING id`, workspaceID, communityID, body.Name).Scan(&planID)
	if err != nil {
		http.Error(w, "plan name unavailable", http.StatusConflict)
		return
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO plan_versions(plan_id,version,price_stars,description,benefits,terms)
		VALUES($1,1,$2,$3,$4,$5) RETURNING id`, planID, body.PriceStars, body.Description, body.Benefits, body.Terms).Scan(&versionID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 201, map[string]any{"id": planID, "version_id": versionID, "version": 1})
}

func (a *app) revisePlan(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var terms models.PlanTerms
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&terms) != nil || terms.Validate() != nil {
		http.Error(w, "invalid plan terms", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var planID string
	var nextVersion int
	err = tx.QueryRow(r.Context(), `SELECT p.id FROM plans p
		JOIN workspace_members wm ON wm.workspace_id=p.workspace_id AND wm.telegram_user_id=$2
		JOIN communities c ON c.id=p.community_id
		WHERE p.id::text=$1 AND wm.role IN ('owner','admin') AND p.status<>'archived' AND c.billing_mode='managed_bot'
		FOR UPDATE OF p`, r.PathValue("id"), userID).Scan(&planID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.QueryRow(r.Context(), `SELECT coalesce(max(version),0)+1 FROM plan_versions WHERE plan_id=$1`, planID).Scan(&nextVersion); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var versionID string
	err = tx.QueryRow(r.Context(), `INSERT INTO plan_versions(plan_id,version,price_stars,description,benefits,terms)
		VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, planID, nextVersion, terms.PriceStars, terms.Description, terms.Benefits, terms.Terms).Scan(&versionID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 201, map[string]any{"plan_id": planID, "version_id": versionID, "version": nextVersion})
}

func (a *app) setPlanStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body) != nil || (body.Status != "active" && body.Status != "paused" && body.Status != "archived") {
		http.Error(w, "invalid status", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var planID, creatorID string
	err = tx.QueryRow(r.Context(), `UPDATE plans p SET status=$3 FROM workspaces ws
		JOIN workspace_members wm ON wm.workspace_id=ws.id AND wm.telegram_user_id=$2
		JOIN communities c ON c.workspace_id=ws.id AND c.billing_mode='managed_bot'
		WHERE p.id::text=$1 AND p.workspace_id=ws.id AND p.community_id=c.id AND wm.role IN ('owner','admin') AND p.status<>'archived'
		RETURNING p.id,(SELECT creator_id FROM workspaces WHERE id=p.workspace_id)`, r.PathValue("id"), userID, body.Status).Scan(&planID, &creatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("plan status update failed", "error", err)
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
		VALUES($1,$2,'plan_status_changed',jsonb_build_object('plan_id',$3::text,'status',$4::text))`, creatorID, userID, planID, body.Status); err != nil {
		slog.Error("plan status audit failed", "error", err)
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("plan status commit failed", "error", err)
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"id": planID, "status": body.Status})
}

func (a *app) creatorPlans(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	communityID := r.PathValue("id")
	var mode string
	err = a.db.QueryRow(r.Context(), `SELECT c.billing_mode FROM communities c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id
		WHERE c.id::text=$1 AND wm.telegram_user_id=$2 AND wm.role IN ('owner','admin')`, communityID, userID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT p.id,p.name,p.status,v.id,v.version,v.price_stars,v.description,v.benefits,v.terms
		FROM plans p JOIN LATERAL (SELECT * FROM plan_versions WHERE plan_id=p.id ORDER BY version DESC LIMIT 1) v ON true
		WHERE p.community_id::text=$1 ORDER BY p.created_at,p.id`, communityID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, status, versionID, description, benefits, terms string
		var version, price int
		if err := rows.Scan(&id, &name, &status, &versionID, &version, &price, &description, &benefits, &terms); err != nil {
			http.Error(w, "database error", 500)
			return
		}
		result = append(result, map[string]any{"id": id, "name": name, "status": status, "version_id": versionID, "version": version,
			"price_stars": price, "period_seconds": models.SubscriptionPeriodSeconds, "description": description, "benefits": benefits, "terms": terms})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"community_id": communityID, "mode": mode, "plans": result})
}
