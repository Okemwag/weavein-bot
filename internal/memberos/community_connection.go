package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"memberos/internal/telegram"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
		http.Error(w, "MemberOS memberships aren't available right now", http.StatusServiceUnavailable)
		return
	}
	var creatorID, workspaceID string
	err = a.db.QueryRow(r.Context(), `SELECT w.creator_id,w.id FROM workspaces w JOIN workspace_members wm ON wm.workspace_id=w.id
		WHERE w.id=$1 AND wm.telegram_user_id=$2 AND wm.role='owner'`, r.PathValue("id"), ownerID).Scan(&creatorID, &workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if body.Mode == "managed_bot" && a.cfg.managedWorkspaceID != workspaceID {
		http.Error(w, "MemberOS memberships aren't turned on for your account yet. Contact the MemberOS team", http.StatusForbidden)
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
		http.Error(w, "Your account can't add another community right now. Contact the MemberOS team", http.StatusForbidden)
		return
	}
	var exists bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM communities WHERE telegram_chat_id=$1)`, body.ChatID).Scan(&exists); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if exists {
		http.Error(w, "This channel is already connected to MemberOS", http.StatusConflict)
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
	if err := a.telegram.Call(r.Context(), method, params, &link); err != nil {
		var apiErr telegram.StatusError
		if errors.As(err, &apiErr) {
			slog.Warn("Telegram invite link rejected", "chat_id", body.ChatID, "method", method,
				"status", apiErr.Code, "reason", apiErr.Description)
			if code, issue := chatAccessError(err); code == http.StatusUnprocessableEntity &&
				strings.Contains(strings.ToLower(apiErr.Description), "chat not found") {
				http.Error(w, issue.Error(), code)
				return
			}
		}
		http.Error(w, "Telegram couldn't create a join link. Check that MemberOS can invite users to your channel, then try again", http.StatusBadGateway)
		return
	}
	if link.InviteLink == "" {
		slog.Error("Telegram returned an empty invite link", "chat_id", body.ChatID, "method", method)
		http.Error(w, "Telegram didn't return a join link. Please try again in a moment", http.StatusBadGateway)
		return
	}
	linkPersisted := false
	defer func() {
		if linkPersisted {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var revoked struct {
			InviteLink string `json:"invite_link"`
		}
		if err := a.telegram.Call(ctx, "revokeChatInviteLink", url.Values{
			"chat_id": {strconv.FormatInt(body.ChatID, 10)}, "invite_link": {link.InviteLink},
		}, &revoked); err != nil {
			slog.Error("orphan Telegram invite link could not be revoked", "chat_id", body.ChatID, "error", err)
		}
	}()
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var currentStatus string
	var currentLimit int
	var currentManagedAllowed bool
	err = tx.QueryRow(r.Context(), `SELECT status,community_limit,managed_billing_allowed FROM saas_accounts
		WHERE workspace_id=$1 FOR UPDATE`, workspaceID).Scan(&currentStatus, &currentLimit, &currentManagedAllowed)
	if err != nil {
		http.Error(w, "SaaS account unavailable", http.StatusServiceUnavailable)
		return
	}
	var currentUsed int
	if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM communities
		WHERE workspace_id=$1 AND status<>'archived'`, workspaceID).Scan(&currentUsed); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if currentStatus != "active" || currentUsed >= currentLimit || (body.Mode == "managed_bot" && !currentManagedAllowed) {
		http.Error(w, "Your account can't add another community right now. Contact the MemberOS team", http.StatusForbidden)
		return
	}
	var communityID string
	if body.Mode == "native_channel" {
		err = tx.QueryRow(r.Context(), `INSERT INTO communities(creator_id,workspace_id,telegram_chat_id,title,description,native_invite_link,price_stars,billing_mode,bot_admin_verified_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,'native_channel',now()) RETURNING id`, creatorID, workspaceID, body.ChatID, body.Title, body.Description, link.InviteLink, body.PriceStars).Scan(&communityID)
	} else {
		err = tx.QueryRow(r.Context(), `INSERT INTO communities(creator_id,workspace_id,telegram_chat_id,title,description,managed_join_link,price_stars,billing_mode,billing_bot_id,bot_admin_verified_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,'managed_bot',$8,now()) RETURNING id`, creatorID, workspaceID, body.ChatID, body.Title, body.Description, link.InviteLink, body.PriceStars, a.cfg.botID).Scan(&communityID)
	}
	if err != nil {
		http.Error(w, "This channel is already connected to MemberOS", http.StatusConflict)
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
		// A lost connection can make COMMIT's outcome ambiguous. Never revoke an
		// invite link that may already be referenced by a committed community.
		checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var saved bool
		checkErr := a.db.QueryRow(checkCtx, `SELECT EXISTS(SELECT 1 FROM communities
			WHERE telegram_chat_id=$1 AND (native_invite_link=$2 OR managed_join_link=$2))`,
			body.ChatID, link.InviteLink).Scan(&saved)
		cancel()
		linkPersisted = saved || checkErr != nil
		if checkErr != nil {
			slog.Error("community commit outcome uncertain; invite link retained", "chat_id", body.ChatID, "error", checkErr)
		} else if saved {
			slog.Warn("community committed despite response failure", "chat_id", body.ChatID)
		}
		http.Error(w, "We're not sure that finished. Reopen this screen to check before trying again", http.StatusServiceUnavailable)
		return
	}
	linkPersisted = true
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
		WHERE c.id=$1`, r.PathValue("id"), ownerID).Scan(&communityID, &chatID, &mode, &status, &workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if status == "archived" {
		http.Error(w, "This community was closed and can't be reconnected", http.StatusConflict)
		return
	}
	if mode == "managed_bot" && (!a.cfg.managedEnabled || a.cfg.botRecipientAck != strconv.FormatInt(a.cfg.botID, 10) || a.cfg.managedWorkspaceID != workspaceID) {
		http.Error(w, "MemberOS memberships aren't turned on for your account yet. Contact the MemberOS team", http.StatusForbidden)
		return
	}
	if code, err := a.verifyCommunityPermissions(r.Context(), chatID, ownerID, mode); err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	if err := a.publishProvisioned(r.Context(), communityID, a.cfg.publicWebhookURL); err != nil {
		http.Error(w, "We couldn't finish connecting to Telegram. Please try again in a moment", http.StatusBadGateway)
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
			VALUES($1,'community_connection_repaired',jsonb_build_object('community_id',$2::uuid))`, ownerID, communityID); err != nil {
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
