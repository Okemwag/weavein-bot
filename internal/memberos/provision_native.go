package memberos

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// provision creates one native paid-channel link after verifying the bot and
// creator against Telegram. Run it explicitly; normal service startup is read-only.
func (a *app) provision(ctx context.Context) error {
	creatorUserID, err := positiveEnvID("CREATOR_TELEGRAM_ID")
	if err != nil {
		return err
	}
	chatID, err := positiveOrNegativeEnvID("CHANNEL_CHAT_ID")
	if err != nil {
		return err
	}
	price, err := strconv.Atoi(os.Getenv("CHANNEL_PRICE_STARS"))
	if err != nil || price < 1 || price > 10000 {
		return errors.New("CHANNEL_PRICE_STARS must be 1-10000")
	}
	name := strings.TrimSpace(os.Getenv("CREATOR_NAME"))
	support := strings.TrimSpace(os.Getenv("SUPPORT_CONTACT"))
	title := strings.TrimSpace(os.Getenv("CHANNEL_TITLE"))
	description := strings.TrimSpace(os.Getenv("CHANNEL_DESCRIPTION"))
	webhookURL := strings.TrimSpace(os.Getenv("PUBLIC_WEBHOOK_URL"))
	if name == "" || support == "" || title == "" || !strings.HasPrefix(webhookURL, "https://") {
		return errors.New("CREATOR_NAME, SUPPORT_CONTACT, CHANNEL_TITLE and HTTPS PUBLIC_WEBHOOK_URL are required")
	}
	var existingID, existingStatus string
	var existingPrice int
	err = a.db.QueryRow(ctx, `SELECT c.id,c.status,c.price_stars FROM communities c
        JOIN workspace_members wm ON wm.workspace_id=c.workspace_id AND wm.role='owner'
        WHERE c.telegram_chat_id=$1 AND wm.telegram_user_id=$2`, chatID, creatorUserID).Scan(&existingID, &existingStatus, &existingPrice)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if existingStatus == "published" {
		return errors.New("channel already provisioned")
	}
	if existingStatus == "draft" && existingPrice != price {
		return errors.New("draft channel price differs from requested price")
	}
	if _, err := a.verifyCommunityPermissions(ctx, chatID, creatorUserID, "native_channel"); err != nil {
		return err
	}
	if existingStatus == "draft" {
		return a.publishProvisioned(ctx, existingID, webhookURL)
	}
	var link struct {
		InviteLink string `json:"invite_link"`
	}
	if err := a.telegram.Call(ctx, "createChatSubscriptionInviteLink", url.Values{
		"chat_id":             {strconv.FormatInt(chatID, 10)},
		"subscription_period": {"2592000"},
		"subscription_price":  {strconv.Itoa(price)},
		"name":                {"MemberOS"},
	}, &link); err != nil {
		return err
	}
	if link.InviteLink == "" {
		return errors.New("telegram returned empty subscription link")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	creatorID, workspaceID, err := createProvisionWorkspace(ctx, tx, name, support, creatorUserID)
	if err != nil {
		return err
	}
	var communityID string
	if err := tx.QueryRow(ctx, `INSERT INTO communities(creator_id,workspace_id,telegram_chat_id,title,description,native_invite_link,price_stars,bot_admin_verified_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, creatorID, workspaceID, chatID, title, description, link.InviteLink, price, time.Now()).Scan(&communityID); err != nil {
		return err
	}
	if err := createDefaultPlan(ctx, tx, workspaceID, communityID, price, description, "Telegram manages this 30-day channel subscription, billing and access."); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return a.publishProvisioned(ctx, communityID, webhookURL)
}
