package memberos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"
)

func (a *app) runOperationsAlerts(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		if err := a.recoverInterruptedRefunds(ctx); err != nil {
			slog.Error("refund recovery check failed", "error", err)
		}
		if err := a.queueOperationsAlerts(ctx); err != nil {
			slog.Error("operations alert check failed", "error", err)
		}
		if err := a.runSaaSLifecycle(ctx); err != nil {
			slog.Error("MemberOS Pro lifecycle check failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *app) recoverInterruptedRefunds(ctx context.Context) error {
	_, err := a.db.Exec(ctx, `WITH interrupted AS (
		UPDATE refund_requests SET status='needs_review',
			decision_note='Refund submission interrupted; verify Telegram outcome before any retry',updated_at=now()
		WHERE status='processing' AND updated_at<now()-interval '2 minutes'
		RETURNING id,payment_id
	) INSERT INTO audit_events(action,details)
	SELECT 'refund_submission_interrupted',jsonb_build_object('request_id',id,'payment_id',payment_id)
	FROM interrupted`)
	return err
}

func (a *app) queueOperationsAlerts(ctx context.Context) error {
	if a.cfg.adminTelegramID <= 0 {
		return nil
	}
	var delayedUpdates, failedUpdates, failedAccess, paymentGaps, overdueTickets, failedNotices, uncertainNotices, uncertainRefunds, duplicates int
	err := a.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM telegram_updates WHERE state='pending' AND received_at<now()-interval '5 minutes'),
		(SELECT count(*) FROM telegram_updates WHERE state IN ('failed','quarantined','answer_unknown')),
		(SELECT count(*) FROM access_jobs WHERE status='failed'),
		(SELECT count(*) FROM notifications WHERE status='failed'),
		(SELECT count(*) FROM notifications WHERE status IN ('sending','uncertain') AND last_attempt_at<now()-interval '2 minutes'),
		(SELECT count(*) FROM refund_requests WHERE status='needs_review'),
		(SELECT count(*) FROM support_tickets WHERE status<>'resolved' AND escalated_at IS NULL
			AND created_at<now()-interval '48 hours'),
		(SELECT count(*) FROM payments p WHERE p.billing_bot_id=$1 AND p.paid_at <
			(SELECT max(finished_at) FROM star_reconciliation_runs WHERE bot_id=$1 AND status='complete')
			AND NOT EXISTS(SELECT 1 FROM observed_star_transactions t WHERE t.bot_id=p.billing_bot_id
			AND t.transaction_id=p.telegram_charge_id AND t.direction='incoming')),
		(SELECT count(*) FROM (SELECT 1 FROM subscriptions WHERE state NOT IN ('expired','refunded') AND paid_through>now()
			GROUP BY community_id,telegram_user_id HAVING count(*)>1) d)`, a.cfg.botID).
		Scan(&delayedUpdates, &failedUpdates, &failedAccess, &failedNotices, &uncertainNotices, &uncertainRefunds, &overdueTickets, &paymentGaps, &duplicates)
	if err != nil {
		return err
	}
	if delayedUpdates+failedUpdates+failedAccess+failedNotices+uncertainNotices+uncertainRefunds+paymentGaps+overdueTickets+duplicates == 0 {
		return nil
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message := fmt.Sprintf("MemberOS needs a look. Open Admin for details:\n%d events delayed\n%d events failed\n%d membership changes failed\n%d messages failed\n%d messages unconfirmed\n%d refunds to review\n%d requests over 48 hours old\n%d payments missing from the Stars check\n%d members with duplicate subscriptions",
		delayedUpdates, failedUpdates, failedAccess, failedNotices, uncertainNotices, uncertainRefunds, overdueTickets, paymentGaps, duplicates)
	slog.Warn("operations need review", "summary", message)
	// The key changes only with the counts or the day, so an unchanged backlog
	// produces one reminder a day instead of an alert every hour. The operator is
	// messaged directly: a missing contact record must not silence the alert.
	sum := sha256.Sum256([]byte(message))
	key := "ops-alert:" + time.Now().UTC().Format("2006-01-02") + ":" + hex.EncodeToString(sum[:8])
	if _, err := tx.Exec(ctx, `INSERT INTO notifications(bot_id,telegram_user_id,dedupe_key,message)
		VALUES($1,$2,$3,$4) ON CONFLICT (dedupe_key) DO NOTHING`, a.cfg.botID, a.cfg.adminTelegramID, key, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
