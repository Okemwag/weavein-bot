package access

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

func (s Service) RunExpirations(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		if err := s.ExpireDue(ctx); err != nil {
			slog.Error("expiration reconciliation failed", "error", err)
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
	rows, err := tx.Query(ctx, `UPDATE entitlements SET status='expired',updated_at=now()
        WHERE status='active' AND valid_until<=now()
        RETURNING subscription_id,community_id,telegram_user_id,valid_until`)
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
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET state='expired',updated_at=now()
            WHERE id=$1 AND paid_through<=now() AND state NOT IN ('expired','refunded')`, item.subID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
            VALUES($1,$2,'remove',$3) ON CONFLICT (dedupe_key) DO NOTHING`, item.communityID, item.userID,
			fmt.Sprintf("remove:expiry:%s:%d", item.subID, item.until.Unix())); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
