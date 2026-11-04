package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"memberos/internal/eventing"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) processCommand(ctx context.Context, tx pgx.Tx, botID, updateID int64, raw []byte) error {
	var update struct {
		Message struct {
			Text string `json:"text"`
			Chat struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &update) != nil || update.Message.Chat.Type != "private" || update.Message.Chat.ID != update.Message.From.ID || update.Message.From.ID <= 0 {
		return eventing.InvalidEvent("invalid private command")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bot_private_contacts(bot_id,telegram_user_id)
        VALUES($1,$2) ON CONFLICT (bot_id,telegram_user_id)
        DO UPDATE SET last_contact_at=now()`, botID, update.Message.From.ID); err != nil {
		return err
	}
	message := ""
	switch update.Message.Text {
	case "/start":
		message = "Welcome to MemberOS. Open the Mini App to discover communities and manage your memberships. Use /paysupport for payment or access help."
	case "/help":
		message = "MemberOS helps you manage paid community access. Open the Mini App to view your memberships. For payment or access help, send /paysupport."
	case "/support", "/paysupport":
		message = "For payment or access help, open the MemberOS Mini App, select your community, and submit a support request. Include the payment date and what went wrong."
	case "/terms":
		message = "Review the seller, renewal terms, cancellation terms, and support contact on the community plan before paying in the MemberOS Mini App."
	default:
		return eventing.InvalidEvent("unsupported command")
	}
	_, err := tx.Exec(ctx, `INSERT INTO notifications(bot_id,telegram_user_id,dedupe_key,message)
        VALUES($1,$2,$3,$4) ON CONFLICT (dedupe_key) DO NOTHING`, botID, update.Message.From.ID, "command:"+strconv.FormatInt(updateID, 10), message)
	return err
}

func (a *app) sendNotifications(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 10; i++ {
				more, err := a.sendOneNotification(ctx)
				if err != nil {
					slog.Error("notification send failed", "error", err)
					break
				}
				if !more {
					break
				}
			}
		}
	}
}

func (a *app) sendOneNotification(ctx context.Context) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, message string
	var bot, user int64
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id,bot_id,telegram_user_id,message,attempts FROM notifications
        WHERE status='pending' AND run_after<=now() ORDER BY run_after,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &bot, &user, &message, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if bot != a.cfg.botID {
		_, err = tx.Exec(ctx, `UPDATE notifications SET status='failed',last_error='bot mismatch' WHERE id=$1`, id)
		if err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	err = a.telegram.Call(ctx, "sendMessage", url.Values{"chat_id": {strconv.FormatInt(user, 10)}, "text": {message}}, &sent)
	if err != nil || sent.MessageID <= 0 {
		status := "pending"
		if attempts+1 >= 5 {
			status = "failed"
		}
		_, dbErr := tx.Exec(ctx, `UPDATE notifications SET status=$2,attempts=attempts+1,last_error='Telegram delivery failed',
            run_after=now()+make_interval(secs => $3::int) WHERE id=$1`, id, status, 1<<min(attempts+1, 6))
		if dbErr != nil {
			return false, dbErr
		}
		return true, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE notifications SET status='sent',sent_at=now(),attempts=attempts+1,last_error=NULL WHERE id=$1`, id)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
