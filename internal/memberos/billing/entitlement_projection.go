package billing

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func refreshEntitlement(ctx context.Context, tx pgx.Tx, subscriptionID, communityID string, userID int64) error {
	var maxUntil *time.Time
	if err := tx.QueryRow(ctx, `SELECT max(provider_expiration) FROM payments WHERE subscription_id=$1 AND refunded_at IS NULL`, subscriptionID).Scan(&maxUntil); err != nil {
		return err
	}
	if maxUntil == nil {
		_, err := tx.Exec(ctx, `DELETE FROM entitlements WHERE subscription_id=$1`, subscriptionID)
		return err
	}
	status := "expired"
	if maxUntil.After(time.Now()) {
		status = "active"
	}
	_, err := tx.Exec(ctx, `INSERT INTO entitlements(subscription_id,community_id,telegram_user_id,valid_until,status)
        VALUES($1,$2,$3,$4,$5) ON CONFLICT (subscription_id) DO UPDATE SET
        valid_until=EXCLUDED.valid_until,status=EXCLUDED.status,updated_at=now()`, subscriptionID, communityID, userID, maxUntil, status)
	return err
}
