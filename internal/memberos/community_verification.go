package memberos

import (
	"context"
	"errors"
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
		return 502, err
	}
	if owner.Status != "creator" {
		return 422, errors.New("you must own this Telegram chat")
	}
	admin, err := a.telegram.GetChatMember(ctx, chatID, a.cfg.botID)
	if err != nil {
		return 502, err
	}
	if admin.Status != "administrator" || !admin.CanInviteUsers || (mode == "managed_bot" && !admin.CanRestrictMembers) {
		return 422, errors.New("bot administrator rights are missing: invite users and, for managed billing, restrict members")
	}
	var chat struct {
		Type     string `json:"type"`
		Username string `json:"username"`
	}
	if err := a.telegram.Call(ctx, "getChat", url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}}, &chat); err != nil {
		return 502, err
	}
	if chat.Username != "" {
		return 422, errors.New("the Telegram chat must be private")
	}
	if mode == "native_channel" && chat.Type != "channel" {
		return 422, errors.New("native paid subscriptions require a private channel")
	}
	if mode == "managed_bot" && chat.Type != "channel" && chat.Type != "supergroup" {
		return 422, errors.New("managed billing requires a private channel or supergroup")
	}
	return 0, nil
}
