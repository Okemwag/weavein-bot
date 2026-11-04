package memberos

import (
	"context"
	"strconv"
	"strings"
	"time"
)

func (a *app) webhookHealth(ctx context.Context) map[string]any {
	var info struct {
		URL              string   `json:"url"`
		Pending          int      `json:"pending_update_count"`
		LastErrorDate    int64    `json:"last_error_date"`
		LastErrorMessage string   `json:"last_error_message"`
		AllowedUpdates   []string `json:"allowed_updates"`
	}
	if err := a.telegram.Call(ctx, "getWebhookInfo", nil, &info); err != nil {
		return map[string]any{"status": "telegram_unavailable"}
	}
	expected := ""
	if strings.HasPrefix(a.cfg.publicWebhookURL, "https://") {
		expected = strings.TrimRight(a.cfg.publicWebhookURL, "/") + "/api/v1/telegram/webhooks/" + strconv.FormatInt(a.cfg.botID, 10)
	}
	status := "healthy"
	if expected == "" {
		status = "origin_unconfigured"
	} else if info.URL != expected {
		status = "webhook_mismatch"
	}
	if status == "healthy" && info.LastErrorDate > 0 && time.Unix(info.LastErrorDate, 0).After(time.Now().Add(-15*time.Minute)) {
		status = "recent_delivery_error"
	}
	if status == "healthy" && info.Pending > 100 {
		status = "delivery_backlog"
	}
	var lastErrorAt *time.Time
	if info.LastErrorDate > 0 {
		value := time.Unix(info.LastErrorDate, 0).UTC()
		lastErrorAt = &value
	}
	return map[string]any{"status": status, "configured_url": info.URL, "pending_updates": info.Pending,
		"last_error_at": lastErrorAt, "last_error": info.LastErrorMessage,
		"allowed_updates": info.AllowedUpdates}
}
