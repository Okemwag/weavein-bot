package memberos

import (
	"context"
	"fmt"
	"log/slog"
	"memberos/internal/notify"
	"time"
)

func (a *app) runOperationsAlerts(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		if err := a.queueOperationsAlerts(ctx); err != nil {
			slog.Error("operations alert check failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *app) queueOperationsAlerts(ctx context.Context) error {
	if a.cfg.adminTelegramID <= 0 {
		return nil
	}
	var delayedUpdates, failedUpdates, failedAccess, paymentGaps int
	err := a.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM telegram_updates WHERE state='pending' AND received_at<now()-interval '5 minutes'),
		(SELECT count(*) FROM telegram_updates WHERE state IN ('failed','quarantined')),
		(SELECT count(*) FROM access_jobs WHERE status='failed'),
		(SELECT count(*) FROM payments p WHERE p.billing_bot_id=$1 AND p.paid_at <
			(SELECT max(finished_at) FROM star_reconciliation_runs WHERE bot_id=$1 AND status='complete')
			AND NOT EXISTS(SELECT 1 FROM observed_star_transactions t WHERE t.bot_id=p.billing_bot_id
			AND t.transaction_id=p.telegram_charge_id AND t.direction='incoming'))`, a.cfg.botID).
		Scan(&delayedUpdates, &failedUpdates, &failedAccess, &paymentGaps)
	if err != nil {
		return err
	}
	if delayedUpdates+failedUpdates+failedAccess+paymentGaps == 0 {
		return nil
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message := fmt.Sprintf("MemberOS operations need review: %d delayed updates, %d failed or quarantined updates, %d failed access jobs, %d recorded payments absent from the latest completed Stars reconciliation. Open Ops for details.",
		delayedUpdates, failedUpdates, failedAccess, paymentGaps)
	key := "ops-alert:" + time.Now().UTC().Format("2006-01-02T15")
	if err := notify.Queue(ctx, tx, a.cfg.botID, a.cfg.adminTelegramID, key, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
