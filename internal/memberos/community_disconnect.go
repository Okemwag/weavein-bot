package memberos

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Disconnection is limited to communities without checkout or subscription
// history. Existing recurring billing requires an operator-led migration.
func (a *app) disconnectCommunity(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
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

	var id, creator, link string
	var chat int64
	err = tx.QueryRow(r.Context(), `SELECT c.id,c.creator_id,c.telegram_chat_id,c.managed_join_link
		FROM communities c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id
		WHERE c.id::text=$1 AND wm.telegram_user_id=$2 AND wm.role='owner'
		AND c.billing_mode='managed_bot' AND c.status='published' FOR UPDATE OF c`,
		r.PathValue("id"), user).Scan(&id, &creator, &chat, &link)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var historyCount int
	err = tx.QueryRow(r.Context(), `SELECT
		(SELECT count(*) FROM subscriptions WHERE community_id=$1)
		+(SELECT count(*) FROM checkout_attempts WHERE community_id=$1)
		+(SELECT count(*) FROM access_overrides WHERE community_id=$1 AND active=true AND valid_until>now())`, id).Scan(&historyCount)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if historyCount != 0 {
		http.Error(w, "financial history or temporary access requires operator review before disconnect", http.StatusConflict)
		return
	}
	var revoked struct {
		InviteLink string `json:"invite_link"`
	}
	if err := a.telegram.Call(r.Context(), "revokeChatInviteLink", url.Values{
		"chat_id": {strconv.FormatInt(chat, 10)}, "invite_link": {link},
	}, &revoked); err != nil {
		http.Error(w, "Telegram could not revoke the join link", http.StatusBadGateway)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE communities SET status='archived',connection_status='disconnected',
		last_permission_check_at=now(),last_permission_error=NULL WHERE id=$1`, id); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
		VALUES($1,$2,'community_disconnected',jsonb_build_object('community_id',$3))`, creator, user, id); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error; check connection state before retrying", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "disconnected"})
}
