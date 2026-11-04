package memberos

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"memberos/internal/store"
	"net/http"
	"strings"
	"time"
)

func (a *app) creatorAccessOverrides(w http.ResponseWriter, r *http.Request) {
	community, ok := a.creatorManagedCommunity(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,telegram_user_id,valid_until,reason,active,created_by,created_at,revoked_at
 FROM access_overrides WHERE community_id=$1 ORDER BY created_at DESC LIMIT 100`, community)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, reason string
		var user, author int64
		var until, created time.Time
		var revoked *time.Time
		var active bool
		if rows.Scan(&id, &user, &until, &reason, &active, &author, &created, &revoked) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "telegram_user_id": user, "valid_until": until, "reason": reason,
			"active": active && until.After(time.Now()), "created_by": author, "created_at": created, "revoked_at": revoked})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"overrides": items})
}

func (a *app) creatorGrantOverride(w http.ResponseWriter, r *http.Request) {
	community, ok := a.creatorManagedCommunity(w, r)
	if !ok {
		return
	}
	author, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input struct {
		TelegramUserID int64     `json:"telegram_user_id"`
		ValidUntil     time.Time `json:"valid_until"`
		Reason         string    `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || input.TelegramUserID <= 0 ||
		input.ValidUntil.Before(time.Now().Add(time.Hour)) || input.ValidUntil.After(time.Now().Add(90*24*time.Hour)) ||
		len(strings.TrimSpace(input.Reason)) < 10 || len(input.Reason) > 500 {
		http.Error(w, "invalid access override", http.StatusBadRequest)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if err := store.LockAccess(r.Context(), tx, community, input.TelegramUserID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var creator string
	err = tx.QueryRow(r.Context(), `SELECT creator_id FROM communities WHERE id=$1 AND billing_mode='managed_bot'`, community).Scan(&creator)
	if err != nil {
		http.Error(w, "community unavailable", http.StatusConflict)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO members(creator_id,telegram_user_id) VALUES($1,$2)
 ON CONFLICT (creator_id,telegram_user_id) DO NOTHING`, creator, input.TelegramUserID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO access_overrides(community_id,telegram_user_id,valid_until,reason,created_by)
 VALUES($1,$2,$3,$4,$5) RETURNING id`, community, input.TelegramUserID, input.ValidUntil, strings.TrimSpace(input.Reason), author).Scan(&id)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
 VALUES($1,$2,'approve',$3) ON CONFLICT (dedupe_key) DO NOTHING`, community, input.TelegramUserID, "approve:override:"+id)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
 VALUES($1,$2,'access_override_granted',jsonb_build_object('override_id',$3,'community_id',$4,'expires_at',$5,'actor_id',$6))`,
		creator, input.TelegramUserID, id, community, input.ValidUntil, author)
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "valid_until": input.ValidUntil, "status": "grant_pending"})
}

func (a *app) creatorRevokeOverride(w http.ResponseWriter, r *http.Request) {
	community, ok := a.creatorManagedCommunity(w, r)
	if !ok {
		return
	}
	author, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var user int64
	var creator string
	err = tx.QueryRow(r.Context(), `SELECT o.telegram_user_id,c.creator_id FROM access_overrides o
 JOIN communities c ON c.id=o.community_id WHERE o.id::text=$1 AND o.community_id=$2 AND o.active=true
 FOR UPDATE OF o`, r.PathValue("override_id"), community).Scan(&user, &creator)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := store.LockAccess(r.Context(), tx, community, user); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE access_overrides SET active=false,revoked_at=now() WHERE id::text=$1`, r.PathValue("override_id"))
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
 VALUES($1,$2,'remove',$3) ON CONFLICT (dedupe_key) DO NOTHING`, community, user, "remove:override:"+r.PathValue("override_id"))
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
 VALUES($1,$2,'access_override_revoked',jsonb_build_object('override_id',$3,'community_id',$4,'actor_id',$5))`,
		creator, user, r.PathValue("override_id"), community, author)
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "revocation_pending"})
}
