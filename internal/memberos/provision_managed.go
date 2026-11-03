package memberos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) provisionManaged(ctx context.Context) error {
	if !a.cfg.managedEnabled || a.cfg.botRecipientAck != strconv.FormatInt(a.cfg.botID, 10) {
		return errors.New("managed billing requires MANAGED_BILLING_ENABLED=true and BOT_STARS_RECIPIENT_ACK equal to TELEGRAM_BOT_ID")
	}
	ownerID, err := positiveEnvID("CREATOR_TELEGRAM_ID")
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
	var existingID, mode, state string
	var existingPrice int
	err = a.db.QueryRow(ctx, `SELECT c.id,c.billing_mode,c.status,c.price_stars FROM communities c
        JOIN creator_users cu ON cu.creator_id=c.creator_id AND cu.role='owner'
        WHERE c.telegram_chat_id=$1 AND cu.telegram_user_id=$2`, chatID, ownerID).Scan(&existingID, &mode, &state, &existingPrice)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if state == "published" || (state == "draft" && (mode != "managed_bot" || existingPrice != price)) {
		return errors.New("channel already provisioned with different configuration")
	}
	if _, err := a.verifyCommunityPermissions(ctx, chatID, ownerID, "managed_bot"); err != nil {
		return err
	}
	if state == "draft" {
		return a.publishProvisioned(ctx, existingID, webhookURL)
	}
	var link struct {
		InviteLink string `json:"invite_link"`
	}
	if err := a.telegram.Call(ctx, "createChatInviteLink", url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)}, "name": {"MemberOS managed"}, "creates_join_request": {"true"},
	}, &link); err != nil {
		return err
	}
	if link.InviteLink == "" {
		return errors.New("telegram returned empty join link")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	creatorID, workspaceID, err := createProvisionWorkspace(ctx, tx, name, support, ownerID)
	if err != nil {
		return err
	}
	var communityID string
	if err := tx.QueryRow(ctx, `INSERT INTO communities(creator_id,workspace_id,telegram_chat_id,title,description,price_stars,
		billing_mode,managed_join_link,billing_bot_id,bot_admin_verified_at) VALUES($1,$2,$3,$4,$5,$6,'managed_bot',$7,$8,$9) RETURNING id`,
		creatorID, workspaceID, chatID, title, description, price, link.InviteLink, a.cfg.botID, time.Now()).Scan(&communityID); err != nil {
		return err
	}
	if err := createDefaultPlan(ctx, tx, workspaceID, communityID, price, description, "Recurring 30-day Telegram Stars subscription. Cancellation stops future renewals; paid access lasts until the paid-through date."); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := a.publishProvisioned(ctx, communityID, webhookURL); err != nil {
		return fmt.Errorf("managed community %s saved as draft: %w", communityID, err)
	}
	slog.Info("managed community provisioned", "community_id", communityID)
	return nil
}
