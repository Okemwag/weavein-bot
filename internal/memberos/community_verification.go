package memberos

import (
	"context"
	"errors"
	"memberos/internal/telegram"
	"net/http"
	"net/url"
	"strconv"
)

// verifyCommunityPermissions checks the signed-in owner and this deployment's bot
// against Telegram before MemberOS writes or republishes a community.
func (a *app) verifyCommunityPermissions(ctx context.Context, chatID, ownerID int64, mode string) (int, error) {
	var bot struct {
		ID    int64 `json:"id"`
		IsBot bool  `json:"is_bot"`
	}
	if err := a.telegram.Call(ctx, "getMe", nil, &bot); err != nil {
		return 502, err
	}
	if !bot.IsBot || bot.ID != a.cfg.botID {
		return 503, errors.New("configured bot ID does not match token")
	}
	owner, err := a.telegram.GetChatMember(ctx, chatID, ownerID)
	if err != nil {
		return chatAccessError(err)
	}
	if owner.Status != "creator" {
		return 422, userMessage("Only the owner of this channel can connect it. Ask the owner to connect it from their account")
	}
	admin, err := a.telegram.GetChatMember(ctx, chatID, a.cfg.botID)
	if err != nil {
		return chatAccessError(err)
	}
	if admin.Status != "administrator" || !admin.CanInviteUsers || (mode == "managed_bot" && !admin.CanRestrictMembers) {
		return 422, userMessage("MemberOS needs to be an admin of your channel. Make it an admin that can invite users and, for MemberOS memberships, ban users, then try again")
	}
	var chat struct {
		Type     string `json:"type"`
		Username string `json:"username"`
	}
	if err := a.telegram.Call(ctx, "getChat", url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}}, &chat); err != nil {
		return chatAccessError(err)
	}
	if chat.Username != "" {
		return 422, userMessage("This channel is public. Make it private in Telegram, then try again")
	}
	if mode == "native_channel" && chat.Type != "channel" {
		return 422, userMessage("Telegram paid channels only work with a private channel, not a group")
	}
	if mode == "managed_bot" && chat.Type != "channel" && chat.Type != "supergroup" {
		return 422, userMessage("Please choose a private channel or group")
	}
	return 0, nil
}

// userMessage is an error written to be shown directly to the person.
type userMessage string

func (m userMessage) Error() string { return string(m) }

func chatAccessError(err error) (int, error) {
	var apiErr telegram.StatusError
	if errors.As(err, &apiErr) && (apiErr.Code == http.StatusBadRequest || apiErr.Code == http.StatusForbidden) {
		return http.StatusUnprocessableEntity, userMessage("We can't reach this channel yet. Add MemberOS as an admin that can invite users, then try again")
	}
	return http.StatusBadGateway, err
}
