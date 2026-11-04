package memberos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

func (a *app) publishProvisioned(ctx context.Context, communityID, webhookURL string) error {
	var configured bool
	if err := a.telegram.Call(ctx, "setWebhook", url.Values{
		"url":             {strings.TrimRight(webhookURL, "/") + "/api/v1/telegram/webhooks/" + strconv.FormatInt(a.cfg.botID, 10)},
		"secret_token":    {a.cfg.webhookSecret},
		"allowed_updates": {`["chat_member","chat_join_request","message","pre_checkout_query","subscription"]`},
	}, &configured); err != nil {
		return fmt.Errorf("community %s created as draft; webhook setup failed: %w", communityID, err)
	}
	if !configured {
		return errors.New("telegram did not confirm webhook setup")
	}
	if _, err := a.db.Exec(ctx, `UPDATE communities SET status='published',bot_admin_verified_at=now(),
        connection_status='healthy',last_permission_check_at=now(),last_permission_error=NULL WHERE id=$1`, communityID); err != nil {
		return err
	}
	slog.Info("community published", "community_id", communityID)
	return nil
}
