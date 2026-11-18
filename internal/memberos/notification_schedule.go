package memberos

import (
	"context"
	"fmt"
	"log/slog"
	"memberos/internal/notify"
	"time"
)

func (a *app) runReminderSchedule(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		if err := a.queueExpiryReminders(ctx); err != nil {
			slog.Error("expiry reminder schedule failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *app) queueExpiryReminders(ctx context.Context) error {
	rows, err := a.db.Query(ctx, `SELECT e.subscription_id,e.telegram_user_id,e.valid_until,c.title,c.billing_bot_id
 FROM entitlements e JOIN communities c ON c.id=e.community_id
 LEFT JOIN subscriber_notification_preferences pref ON pref.telegram_user_id=e.telegram_user_id
 WHERE e.status='active' AND e.valid_until>now() AND e.valid_until<=now()+interval '3 days'
 AND c.billing_mode='managed_bot' AND c.billing_bot_id=$1
 AND coalesce(pref.reminders_enabled,true) ORDER BY e.valid_until LIMIT 500`, a.cfg.botID)
	if err != nil {
		return err
	}
	type due struct {
		subID, title string
		user, bot    int64
		until        time.Time
	}
	var items []due
	for rows.Next() {
		var item due
		if err := rows.Scan(&item.subID, &item.user, &item.until, &item.title, &item.bot); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range items {
		tx, err := a.db.Begin(ctx)
		if err != nil {
			return err
		}
		message := fmt.Sprintf("Heads up: your %s membership runs until %s. If renewal is on, there's nothing you need to do. If you cancelled it, your access ends on that date.", item.title, item.until.Format("2 Jan 2006"))
		key := fmt.Sprintf("expiry-reminder:%s:%d", item.subID, item.until.Unix())
		if err := notify.Queue(ctx, tx, item.bot, item.user, key, message); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
