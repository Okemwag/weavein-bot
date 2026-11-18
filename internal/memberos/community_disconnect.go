package memberos

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Disconnection is limited to communities without subscription history or active
// temporary access. Existing recurring billing requires an operator-led migration.
// Unpaid checkouts do not count: anyone can start one, and pre-checkout refuses
// payment once the community is archived.
//
// The community is archived first and the link revoked afterwards, with no
// transaction open during the Telegram call. A failed revoke leaves a closed
// community with a recorded gap, and calling this again finishes the job.
func (a *app) disconnectCommunity(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, chat, link, ok := a.archiveCommunity(w, r, user)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()
	var revoked struct {
		InviteLink string `json:"invite_link"`
	}
	if err := a.telegram.Call(ctx, "revokeChatInviteLink", url.Values{
		"chat_id": {strconv.FormatInt(chat, 10)}, "invite_link": {link},
	}, &revoked); err != nil {
		slog.Warn("community archived; join link not revoked", "community_id", id, "error", err)
		_, _ = a.db.Exec(ctx, `UPDATE communities SET last_permission_error='join link revocation not confirmed' WHERE id=$1`, id)
		http.Error(w, "Your community is closed, but Telegram couldn't cancel its join link. Please try again", http.StatusBadGateway)
		return
	}
	if _, err := a.db.Exec(ctx, `UPDATE communities SET join_link_revoked_at=now(),last_permission_error=NULL WHERE id=$1`, id); err != nil {
		slog.Error("join link revoked but not recorded", "community_id", id, "error", err)
	}
	writeJSON(w, 200, map[string]string{"status": "disconnected"})
}

func (a *app) archiveCommunity(w http.ResponseWriter, r *http.Request, user int64) (string, int64, string, bool) {
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return "", 0, "", false
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var id, creator, link, status string
	var chat int64
	err = tx.QueryRow(r.Context(), `SELECT c.id,c.creator_id,c.telegram_chat_id,c.managed_join_link,c.status
		FROM communities c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id
		WHERE c.id=$1 AND wm.telegram_user_id=$2 AND wm.role='owner' AND c.billing_mode='managed_bot'
		AND (c.status='published' OR (c.status='archived' AND c.join_link_revoked_at IS NULL)) FOR UPDATE OF c`,
		r.PathValue("id"), user).Scan(&id, &creator, &chat, &link, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return "", 0, "", false
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return "", 0, "", false
	}
	if status == "archived" {
		return id, chat, link, true
	}
	var historyCount int
	err = tx.QueryRow(r.Context(), `SELECT
		(SELECT count(*) FROM subscriptions WHERE community_id=$1)
		+(SELECT count(*) FROM access_overrides WHERE community_id=$1 AND active=true AND valid_until>now())`, id).Scan(&historyCount)
	if err != nil {
		http.Error(w, "database error", 500)
		return "", 0, "", false
	}
	if historyCount != 0 {
		http.Error(w, "This community has payment history or free access in place, so the MemberOS team needs to close it for you. Please contact us", http.StatusConflict)
		return "", 0, "", false
	}
	if _, err := tx.Exec(r.Context(), `UPDATE communities SET status='archived',connection_status='disconnected',
		last_permission_check_at=now(),last_permission_error=NULL WHERE id=$1`, id); err != nil {
		http.Error(w, "database error", 500)
		return "", 0, "", false
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details)
		VALUES($1,$2,'community_disconnected',jsonb_build_object('community_id',$3::uuid))`, creator, user, id); err != nil {
		http.Error(w, "database error", 500)
		return "", 0, "", false
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error; check connection state before retrying", 500)
		return "", 0, "", false
	}
	return id, chat, link, true
}
