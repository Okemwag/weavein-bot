package memberos

import (
	"context"
	"errors"
	"log/slog"
	"memberos/internal/eventing"
	"memberos/internal/memberos/billing"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) processUpdates(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := a.db.Exec(ctx, `UPDATE telegram_updates SET state='answer_unknown',
				last_error='pre-checkout answer interrupted; inspect payment before any manual action',processed_at=now()
				WHERE state='answering' AND received_at<now()-interval '15 seconds'`); err != nil {
				slog.Error("pre-checkout recovery failed", "error", err)
			}
			for i := 0; i < 20; i++ {
				more, err := a.processOne(ctx)
				if err != nil {
					slog.Error("update processing failed", "error", err)
					break
				}
				if !more {
					break
				}
			}
		}
	}
}

func (a *app) processOne(ctx context.Context) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var botID, updateID, seq int64
	var kind string
	var raw []byte
	var receivedAt time.Time
	err = tx.QueryRow(ctx, `SELECT bot_id,update_id,seq,update_type,payload,received_at FROM telegram_updates
        WHERE state='pending' AND run_after<=now() ORDER BY run_after,seq LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&botID, &updateID, &seq, &kind, &raw, &receivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	state, reason := "processed", ""
	work, err := tx.Begin(ctx)
	if err != nil {
		return false, err
	}
	var processingErr error
	switch kind {
	case "chat_member":
		processingErr = a.processChatMember(ctx, work, updateID, raw)
	case "message":
		processingErr = billing.ProcessMessage(ctx, work, botID, updateID, seq, raw)
	case "command":
		processingErr = a.processCommand(ctx, work, botID, updateID, raw)
	case "contact":
		processingErr = a.processContact(ctx, work, botID, raw)
	case "chat_shared":
		processingErr = a.processChatShared(ctx, work, raw)
	case "subscription":
		processingErr = billing.ProcessSubscription(ctx, work, botID, updateID, seq, raw)
	case "chat_join_request":
		processingErr = a.processJoinRequest(ctx, work, updateID, raw)
	default:
		processingErr = eventing.IgnoredEvent("unsupported Telegram update")
	}
	if processingErr != nil {
		if err := work.Rollback(ctx); err != nil {
			return false, err
		}
		var invalid eventing.InvalidEvent
		var ignored eventing.IgnoredEvent
		var dependency eventing.DependencyPending
		if errors.As(processingErr, &ignored) {
			state, reason = "ignored", string(ignored)
		} else if errors.As(processingErr, &invalid) {
			state, reason = "quarantined", string(invalid)
		} else {
			var attempts int
			if err := tx.QueryRow(ctx, `SELECT attempts FROM telegram_updates WHERE bot_id=$1 AND update_id=$2`, botID, updateID).Scan(&attempts); err != nil {
				return false, err
			}
			state, reason = "pending", processingErr.Error()
			delay := 1 << min(attempts+1, 6)
			if errors.As(processingErr, &dependency) {
				delay = 30 * (1 << min(attempts, 7))
				if delay > 3600 {
					delay = 3600
				}
				if time.Since(receivedAt) >= 24*time.Hour {
					state = "failed"
				}
			} else if attempts+1 >= 5 {
				state = "failed"
			}
			_, err = tx.Exec(ctx, `UPDATE telegram_updates SET state=$3,last_error=$4,attempts=attempts+1,
                run_after=now()+make_interval(secs => $5::int),processed_at=CASE WHEN $3='failed' THEN now() ELSE NULL END
                WHERE bot_id=$1 AND update_id=$2`, botID, updateID, state, reason, delay)
			if err != nil {
				return false, err
			}
			return true, tx.Commit(ctx)
		}
	} else if err := work.Commit(ctx); err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE telegram_updates SET state=$3,last_error=NULLIF($4,''),attempts=attempts+1,processed_at=now()
        WHERE bot_id=$1 AND update_id=$2`, botID, updateID, state, reason)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
