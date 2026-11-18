package access

import (
	"context"
	"fmt"
	"log/slog"
	"memberos/internal/notify"
	"net/url"
	"strings"
	"time"
)

const (
	startupBacklogPoll  = 30 * time.Second
	startupBacklogLimit = 30 * time.Minute
)

func (s Service) RunExpirations(ctx context.Context) {
	s.awaitDeliveryBacklog(ctx)
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		if err := s.ExpireDue(ctx); err != nil {
			slog.Error("expiration reconciliation failed", "error", err)
		}
		if err := s.ExpireOverrides(ctx); err != nil {
			slog.Error("override expiration failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s Service) ExpireDue(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `UPDATE entitlements e SET status='expired',updated_at=now()
        FROM subscriptions s WHERE s.id=e.subscription_id AND e.status='active'
        AND NOT entitlement_is_current(e.status,e.valid_until,s.auto_renew_state,s.state)
        RETURNING e.subscription_id,e.community_id,e.telegram_user_id,e.valid_until`)
	if err != nil {
		return err
	}
	type due struct {
		subID, communityID string
		userID             int64
		until              time.Time
	}
	var expired []due
	for rows.Next() {
		var item due
		if err := rows.Scan(&item.subID, &item.communityID, &item.userID, &item.until); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range expired {
		if _, err := tx.Exec(ctx, `INSERT INTO subscription_events(subscription_id,event_key,kind,details)
            VALUES($1,$2,'entitlement_expired',jsonb_build_object('valid_until',$3::timestamptz))
            ON CONFLICT (event_key) DO NOTHING`, item.subID,
			fmt.Sprintf("expiry:%s:%d", item.subID, item.until.Unix()), item.until); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET state='expired',updated_at=now()
            WHERE id=$1 AND paid_through<=now() AND state NOT IN ('expired','refunded')`, item.subID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
            VALUES($1,$2,'remove',$3) ON CONFLICT (dedupe_key) DO NOTHING`, item.communityID, item.userID,
			fmt.Sprintf("remove:expiry:%s:%d", item.subID, item.until.Unix())); err != nil {
			return err
		}
		message := "Your membership has ended, so your access is ending too. You can join again anytime in MemberOS. Think this is a mistake? Tap Need help? in MemberOS."
		if strings.HasPrefix(s.AppOrigin, "https://") {
			message += "\n\nJoin again: " + strings.TrimRight(s.AppOrigin, "/") + "/app/?community=" + url.QueryEscape(item.communityID)
		}
		if err := notify.Queue(ctx, tx, s.BotID, item.userID,
			fmt.Sprintf("expired:%s:%d", item.subID, item.until.Unix()), message); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// awaitDeliveryBacklog holds removals back after a restart until Telegram has
// redelivered queued updates. Renewals that were paid during the downtime are in
// that queue, and acting first would remove members who have already paid.
func (s Service) awaitDeliveryBacklog(ctx context.Context) {
	deadline := time.Now().Add(startupBacklogLimit)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(startupBacklogPoll):
		}
		if s.deliveryBacklogCleared(ctx) || time.Now().After(deadline) {
			return
		}
	}
}

func (s Service) deliveryBacklogCleared(ctx context.Context) bool {
	var info struct {
		Pending int `json:"pending_update_count"`
	}
	if err := s.Telegram.Call(ctx, "getWebhookInfo", nil, &info); err != nil || info.Pending > 0 {
		return false
	}
	var local int
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM telegram_updates WHERE state='pending' AND run_after<=now()`).Scan(&local)
	return err == nil && local == 0
}
