package memberos

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (a *app) connectCommunity(w http.ResponseWriter, r *http.Request) {
	ownerID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !strings.HasPrefix(a.cfg.publicWebhookURL, "https://") {
		http.Error(w, "PUBLIC_WEBHOOK_URL must be configured with HTTPS", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		ChatID      int64  `json:"telegram_chat_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		PriceStars  int    `json:"price_stars"`
		Mode        string `json:"billing_mode"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	body.Description = strings.TrimSpace(body.Description)
	if body.ChatID == 0 || len(body.Title) < 1 || len(body.Title) > 120 || len(body.Description) > 2000 || body.PriceStars < 1 || body.PriceStars > 10000 || (body.Mode != "native_channel" && body.Mode != "managed_bot") {
		http.Error(w, "invalid community details", 400)
		return
	}
	if body.Mode == "managed_bot" && (!a.cfg.managedEnabled || a.cfg.botRecipientAck != strconv.FormatInt(a.cfg.botID, 10)) {
		http.Error(w, "managed checkout is unavailable for this bot", http.StatusServiceUnavailable)
		return
	}
	var creatorID, workspaceID string
	err = a.db.QueryRow(r.Context(), `SELECT w.creator_id,w.id FROM workspaces w JOIN workspace_members wm ON wm.workspace_id=w.id
		WHERE w.id::text=$1 AND wm.telegram_user_id=$2 AND wm.role='owner'`, r.PathValue("id"), ownerID).Scan(&creatorID, &workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if body.Mode == "managed_bot" && a.cfg.managedWorkspaceID != workspaceID {
		http.Error(w, "managed billing is not enabled for this workspace", http.StatusForbidden)
		return
	}
	var saasActive, managedAllowed bool
	var used, communityLimit int
	err = a.db.QueryRow(r.Context(), `SELECT sa.status='active',sa.managed_billing_allowed,sa.community_limit,
		(SELECT count(*) FROM communities c WHERE c.workspace_id=sa.workspace_id AND c.status<>'archived')
		FROM saas_accounts sa WHERE sa.workspace_id=$1`, workspaceID).
		Scan(&saasActive, &managedAllowed, &communityLimit, &used)
	if err != nil {
		http.Error(w, "SaaS account unavailable", http.StatusServiceUnavailable)
		return
	}
	if !saasActive || used >= communityLimit || (body.Mode == "managed_bot" && !managedAllowed) {
		http.Error(w, "workspace community allowance does not permit this connection", http.StatusForbidden)
		return
	}
	var exists bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM communities WHERE telegram_chat_id=$1)`, body.ChatID).Scan(&exists); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if exists {
		http.Error(w, "chat is already connected; use recheck if it is a draft", http.StatusConflict)
		return
	}
	if code, err := a.verifyCommunityPermissions(r.Context(), body.ChatID, ownerID, body.Mode); err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	var link struct {
		InviteLink string `json:"invite_link"`
	}
	params := url.Values{"chat_id": {strconv.FormatInt(body.ChatID, 10)}, "name": {"MemberOS"}}
	method := "createChatSubscriptionInviteLink"
	if body.Mode == "native_channel" {
		params.Set("subscription_period", "2592000")
		params.Set("subscription_price", strconv.Itoa(body.PriceStars))
	} else {
		method = "createChatInviteLink"
		params.Set("creates_join_request", "true")
	}
	if err := a.telegram.Call(r.Context(), method, params, &link); err != nil || link.InviteLink == "" {
		http.Error(w, "Telegram could not create the invite link", http.StatusBadGateway)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var communityID string
	if body.Mode == "native_channel" {
		err = tx.QueryRow(r.Context(), `INSERT INTO communities(creator_id,workspace_id,telegram_chat_id,title,description,native_invite_link,price_stars,billing_mode,bot_admin_verified_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,'native_channel',now()) RETURNING id`, creatorID, workspaceID, body.ChatID, body.Title, body.Description, link.InviteLink, body.PriceStars).Scan(&communityID)
	} else {
		err = tx.QueryRow(r.Context(), `INSERT INTO communities(creator_id,workspace_id,telegram_chat_id,title,description,managed_join_link,price_stars,billing_mode,billing_bot_id,bot_admin_verified_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,'managed_bot',$8,now()) RETURNING id`, creatorID, workspaceID, body.ChatID, body.Title, body.Description, link.InviteLink, body.PriceStars, a.cfg.botID).Scan(&communityID)
	}
	if err != nil {
		http.Error(w, "chat is already connected or could not be saved", http.StatusConflict)
		return
	}
	terms := "Telegram manages this 30-day channel subscription, billing and access."
	if body.Mode == "managed_bot" {
		terms = "Recurring 30-day Telegram Stars subscription. Cancellation stops future renewals; paid access lasts until the paid-through date."
	}
	if err := createDefaultPlan(r.Context(), tx, workspaceID, communityID, body.PriceStars, body.Description, terms); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,details) VALUES($1,$2,'community_connected',jsonb_build_object('community_id',$3::text))`, creatorID, ownerID, communityID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := a.publishProvisioned(r.Context(), communityID, a.cfg.publicWebhookURL); err != nil {
		slog.Error("community saved as draft; webhook setup failed", "community_id", communityID, "error", err)
		writeJSON(w, 202, map[string]any{"id": communityID, "status": "draft", "next_step": "recheck after webhook configuration is restored"})
		return
	}
	writeJSON(w, 201, map[string]any{"id": communityID, "status": "published"})
}

func (a *app) recheckCommunity(w http.ResponseWriter, r *http.Request) {
	ownerID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !strings.HasPrefix(a.cfg.publicWebhookURL, "https://") {
		http.Error(w, "PUBLIC_WEBHOOK_URL must be configured with HTTPS", http.StatusServiceUnavailable)
		return
	}
	var chatID int64
	var mode, status, communityID, workspaceID string
	err = a.db.QueryRow(r.Context(), `SELECT c.id,c.telegram_chat_id,c.billing_mode,c.status,c.workspace_id FROM communities c
		JOIN workspace_members wm ON wm.workspace_id=c.workspace_id AND wm.telegram_user_id=$2 AND wm.role='owner'
		WHERE c.id::text=$1`, r.PathValue("id"), ownerID).Scan(&communityID, &chatID, &mode, &status, &workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if status == "archived" {
		http.Error(w, "archived community cannot be reconnected", http.StatusConflict)
		return
	}
	if mode == "managed_bot" && (!a.cfg.managedEnabled || a.cfg.botRecipientAck != strconv.FormatInt(a.cfg.botID, 10) || a.cfg.managedWorkspaceID != workspaceID) {
		http.Error(w, "managed billing is not enabled for this workspace", http.StatusForbidden)
		return
	}
	if code, err := a.verifyCommunityPermissions(r.Context(), chatID, ownerID, mode); err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	if err := a.publishProvisioned(r.Context(), communityID, a.cfg.publicWebhookURL); err != nil {
		http.Error(w, "webhook configuration failed; retry later", http.StatusBadGateway)
		return
	}
	if mode == "managed_bot" {
		tx, err := a.db.Begin(r.Context())
		if err != nil {
			http.Error(w, "connection repaired; retry queue unavailable", 500)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()
		if _, err := tx.Exec(r.Context(), `UPDATE access_jobs SET status='pending',attempts=0,
			last_error=NULL,finished_at=NULL,run_after=now() WHERE community_id=$1 AND status='failed'`, communityID); err != nil {
			http.Error(w, "connection repaired; retry queue unavailable", 500)
			return
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
			VALUES($1,'community_connection_repaired',jsonb_build_object('community_id',$2))`, ownerID, communityID); err != nil {
			http.Error(w, "connection repaired; audit unavailable", 500)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			http.Error(w, "connection repaired; retry queue unavailable", 500)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"id": communityID, "status": "published"})
}
