package memberos

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (a *app) creatorCommunityOnboarding(w http.ResponseWriter, r *http.Request) {
	access, ok := a.requireCreatorCommunity(w, r)
	if !ok {
		return
	}
	var rules, guide, category string
	if err := a.db.QueryRow(r.Context(), `SELECT rules,content_guide,category FROM communities WHERE id=$1`, access.ID).
		Scan(&rules, &guide, &category); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"rules": rules, "content_guide": guide, "category": category})
}

func (a *app) updateCommunityOnboarding(w http.ResponseWriter, r *http.Request) {
	access, ok := a.requireCreatorCommunity(w, r)
	if !ok {
		return
	}
	if access.Role != "owner" && access.Role != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var input struct {
		Rules        string `json:"rules"`
		ContentGuide string `json:"content_guide"`
		Category     string `json:"category"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	input.Rules = strings.TrimSpace(input.Rules)
	input.ContentGuide = strings.TrimSpace(input.ContentGuide)
	input.Category = strings.TrimSpace(input.Category)
	if len(input.Rules) > 1800 || len(input.ContentGuide) > 1000 || len(input.Category) > 80 {
		http.Error(w, "community guidance too long", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var creator string
	err = tx.QueryRow(r.Context(), `UPDATE communities SET rules=$2,content_guide=$3,category=$4
		WHERE id=$1 RETURNING creator_id`, access.ID, input.Rules, input.ContentGuide, input.Category).Scan(&creator)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
		VALUES($1,$2,'community_guidance_updated',jsonb_build_object('community_id',$3::uuid))`, creator, access.User, access.ID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"rules": input.Rules, "content_guide": input.ContentGuide, "category": input.Category})
}
