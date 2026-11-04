package memberos

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) submitCreatorInquiry(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name          string `json:"name"`
		Email         string `json:"email"`
		CommunityLink string `json:"community_link"`
		Message       string `json:"message"`
		Website       string `json:"website"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.TrimSpace(input.Email)
	input.CommunityLink = strings.TrimSpace(input.CommunityLink)
	input.Message = strings.TrimSpace(input.Message)
	address, err := mail.ParseAddress(input.Email)
	if input.Website != "" || len(input.Name) < 2 || len(input.Name) > 120 || err != nil || address.Address != input.Email || len(input.Email) > 254 ||
		len(input.Message) < 10 || len(input.Message) > 2000 || len(input.CommunityLink) > 300 {
		http.Error(w, "invalid inquiry", http.StatusBadRequest)
		return
	}
	if input.CommunityLink != "" {
		parsed, err := url.Parse(input.CommunityLink)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			http.Error(w, "invalid community link", http.StatusBadRequest)
			return
		}
	}
	var id string
	if err := a.db.QueryRow(r.Context(), `INSERT INTO creator_inquiries(name,email,community_link,message)
		VALUES($1,$2,$3,$4) RETURNING id`, input.Name, input.Email, input.CommunityLink, input.Message).Scan(&id); err != nil {
		http.Error(w, "inquiry unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "open"})
}

func (a *app) adminCreatorInquiries(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,name,email,community_link,message,created_at
		FROM creator_inquiries WHERE status='open' ORDER BY created_at LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, email, link, message string
		var at time.Time
		if rows.Scan(&id, &name, &email, &link, &message, &at) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "email": email,
			"community_link": link, "message": message, "created_at": at})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"inquiries": items})
}

func (a *app) adminResolveCreatorInquiry(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var input struct {
		Resolution string `json:"resolution"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil || len(strings.TrimSpace(input.Resolution)) < 10 || len(input.Resolution) > 1000 {
		http.Error(w, "invalid resolution", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var id string
	err = tx.QueryRow(r.Context(), `UPDATE creator_inquiries SET status='resolved',resolution=$2,resolved_at=now()
		WHERE id::text=$1 AND status='open' RETURNING id`, r.PathValue("id"), strings.TrimSpace(input.Resolution)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,'creator_inquiry_resolved',jsonb_build_object('inquiry_id',$2))`, operator, id); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "resolved"})
}
