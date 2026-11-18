package memberos

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"memberos/internal/eventing"
	"memberos/internal/telegram"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Chat selection is initiated by an authenticated creator, then completed by
// Telegram's chat_shared service message from that same user in the bot chat.
func (a *app) prepareChatSelection(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var allowed bool
	err = a.db.QueryRow(r.Context(), `SELECT EXISTS(
		SELECT 1 FROM workspace_members wm JOIN saas_accounts sa ON sa.workspace_id=wm.workspace_id
		WHERE wm.workspace_id=$1 AND wm.telegram_user_id=$2 AND wm.role='owner'
		AND sa.status='active' AND
		(SELECT count(*) FROM communities c WHERE c.workspace_id=wm.workspace_id AND c.status<>'archived') < sa.community_limit)`, r.PathValue("id"), userID).Scan(&allowed)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "Your account can't add another community right now", http.StatusForbidden)
		return
	}
	randomID, err := rand.Int(rand.Reader, big.NewInt((1<<31)-1))
	if err != nil {
		http.Error(w, "could not prepare chat selection", http.StatusInternalServerError)
		return
	}
	requestID := randomID.Int64() + 1
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "could not prepare chat selection", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err = tx.Exec(r.Context(), `DELETE FROM chat_selection_requests
		WHERE telegram_user_id=$1 OR created_at<now()-interval '1 day'`, userID); err != nil {
		http.Error(w, "could not prepare chat selection", http.StatusInternalServerError)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM chat_selections WHERE telegram_user_id=$1`, userID); err != nil {
		http.Error(w, "could not prepare chat selection", http.StatusInternalServerError)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO chat_selection_requests(telegram_user_id,request_id) VALUES($1,$2)`, userID, requestID)
	if err != nil {
		http.Error(w, "could not prepare chat selection", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		http.Error(w, "could not prepare chat selection", http.StatusInternalServerError)
		return
	}
	rights := map[string]bool{
		"is_anonymous": false, "can_manage_chat": true, "can_delete_messages": false,
		"can_manage_video_chats": false, "can_restrict_members": false,
		"can_promote_members": false, "can_change_info": false, "can_invite_users": true,
		"can_post_stories": false, "can_edit_stories": false, "can_delete_stories": false,
		"can_post_messages": false, "can_edit_messages": false,
		"can_manage_direct_messages": false, "can_send_welcome_messages": false,
	}
	button, err := json.Marshal(map[string]any{
		"text": "Choose private channel",
		"request_chat": map[string]any{
			"request_id": requestID, "chat_is_channel": true, "chat_has_username": false,
			"chat_is_created": true, "request_title": true,
			"user_administrator_rights": rights, "bot_administrator_rights": rights,
		},
	})
	if err != nil {
		http.Error(w, "could not prepare chat selection", http.StatusInternalServerError)
		return
	}
	var prepared struct {
		ID string `json:"id"`
	}
	err = a.telegram.Call(r.Context(), "savePreparedKeyboardButton", url.Values{
		"user_id": {strconv.FormatInt(userID, 10)}, "button": {string(button)},
	}, &prepared)
	if err != nil || prepared.ID == "" {
		http.Error(w, "Telegram couldn't open the channel picker. Update Telegram or try again", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"request_id": prepared.ID})
}

func (a *app) selectedChat(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var owner bool
	err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM workspace_members
		WHERE workspace_id=$1 AND telegram_user_id=$2 AND role='owner')`, r.PathValue("id"), userID).Scan(&owner)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if !owner {
		http.NotFound(w, r)
		return
	}
	var chatID int64
	var title string
	err = a.db.QueryRow(r.Context(), `SELECT telegram_chat_id,title FROM chat_selections
		WHERE telegram_user_id=$1 AND selected_at>now()-interval '15 minutes'`, userID).Scan(&chatID, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"chat": nil})
		return
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	issue := ""
	if _, err := a.verifyCommunityPermissions(r.Context(), chatID, userID, "native_channel"); err != nil {
		var apiErr telegram.StatusError
		if errors.As(err, &apiErr) {
			issue = "We can't reach Telegram right now. Please try again shortly."
		} else {
			issue = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"chat": map[string]any{"id": chatID, "title": title, "issue": issue}})
}

func (a *app) processChatShared(ctx context.Context, tx pgx.Tx, raw []byte) error {
	var update struct {
		Message struct {
			Chat struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Shared struct {
				RequestID int64  `json:"request_id"`
				ChatID    int64  `json:"chat_id"`
				Title     string `json:"title"`
			} `json:"chat_shared"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &update) != nil || update.Message.Chat.Type != "private" ||
		update.Message.Chat.ID != update.Message.From.ID || update.Message.From.ID <= 0 ||
		update.Message.Shared.RequestID <= 0 || update.Message.Shared.ChatID == 0 {
		return eventing.InvalidEvent("invalid private chat selection")
	}
	var consumed int64
	err := tx.QueryRow(ctx, `DELETE FROM chat_selection_requests
		WHERE telegram_user_id=$1 AND request_id=$2 AND created_at>now()-interval '15 minutes'
		RETURNING request_id`, update.Message.From.ID, update.Message.Shared.RequestID).Scan(&consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.IgnoredEvent("expired or unknown chat selection")
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_selections(telegram_user_id,telegram_chat_id,title)
		VALUES($1,$2,$3) ON CONFLICT (telegram_user_id) DO UPDATE SET
		telegram_chat_id=EXCLUDED.telegram_chat_id,title=EXCLUDED.title,selected_at=now()`,
		update.Message.From.ID, update.Message.Shared.ChatID, update.Message.Shared.Title)
	return err
}
