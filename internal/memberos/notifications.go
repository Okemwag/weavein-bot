package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"memberos/internal/eventing"
	"memberos/internal/telegram"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
		return eventing.IgnoredEvent("command outside a private chat")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bot_private_contacts(bot_id,telegram_user_id)
        VALUES($1,$2) ON CONFLICT (bot_id,telegram_user_id)
        DO UPDATE SET last_contact_at=now()`, botID, update.Message.From.ID); err != nil {
		return err
	}
	message := ""
	switch botCommand(update.Message.Text, a.cfg.botUsername) {
	case "/start":
		message = "Welcome to MemberOS! Tap the Open button to find communities and manage your memberships. Need help with a payment? Send /paysupport."
	case "/help":
		message = "Tap the Open button to see your memberships. Need help with a payment or getting in? Send /paysupport."
	case "/support", "/paysupport":
		message = "To get help, tap the Open button, choose your community and tap Need help? Tell us the payment date and what went wrong."
	case "/terms":
		message = "Each community shows its seller, price, renewal terms and support contact before you pay. Tap the Open button, choose a community and open Billing and support."
	default:
		return eventing.IgnoredEvent("unsupported command")
	}
	_, err := tx.Exec(ctx, `INSERT INTO notifications(bot_id,telegram_user_id,dedupe_key,message)
        VALUES($1,$2,$3,$4) ON CONFLICT (dedupe_key) DO NOTHING`, botID, update.Message.From.ID, "command:"+strconv.FormatInt(updateID, 10), message)
	return err
}

// botCommand extracts a supported form of a command: "/start", "/start payload"
// or "/start@ThisBot". A command addressed to a different bot returns "".
func botCommand(text, username string) string {
	first, _, _ := strings.Cut(text, " ")
	command, target, addressed := strings.Cut(first, "@")
	if addressed && !strings.EqualFold(target, username) {
		return ""
	}
	switch command {
	case "/start", "/help", "/support", "/paysupport", "/terms":
		return command
	}
	return ""
}

// processContact records that a user messaged the bot privately, or allowed it
// to write to them, which is what lets the bot send them notices. The webhook
// stores only the sender for these updates, never the message text.
func (a *app) processContact(ctx context.Context, tx pgx.Tx, botID int64, raw []byte) error {
	var update struct {
		Message struct {
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &update) != nil || update.Message.From.ID <= 0 {
		return eventing.IgnoredEvent("message without a sender")
	}
	_, err := tx.Exec(ctx, `INSERT INTO bot_private_contacts(bot_id,telegram_user_id) VALUES($1,$2)
		ON CONFLICT (bot_id,telegram_user_id) DO UPDATE SET last_contact_at=now()`, botID, update.Message.From.ID)
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
	// A process may stop after Telegram accepted a message but before MemberOS
	// recorded the response. Such attempts require review, never blind replay.
	if _, err := a.db.Exec(ctx, `UPDATE notifications SET status='uncertain',
		last_error='send attempt interrupted; delivery requires review'
		WHERE status='sending' AND last_attempt_at<now()-interval '2 minutes'`); err != nil {
		return false, err
	}
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
	if _, err := tx.Exec(ctx, `UPDATE notifications SET status='sending',attempts=attempts+1,
		last_attempt_at=now(),last_error=NULL WHERE id=$1 AND status='pending'`, id); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	err = a.telegram.Call(ctx, "sendMessage", url.Values{"chat_id": {strconv.FormatInt(user, 10)}, "text": {message}}, &sent)
	status, reason := "sent", ""
	delay := 0
	if err != nil || sent.MessageID <= 0 {
		status, reason, delay = classifySendFailure(err, attempts)
	}
	_, err = a.db.Exec(ctx, `UPDATE notifications SET status=$2,last_error=NULLIF($3,''),
		run_after=now()+make_interval(secs => $4::int),
		sent_at=CASE WHEN $2='sent' THEN now() ELSE sent_at END,
		telegram_message_id=CASE WHEN $2='sent' THEN $5 ELSE telegram_message_id END
		WHERE id=$1 AND status='sending'`, id, status, reason, delay, sent.MessageID)
	if err != nil {
		return false, err
	}
	return true, nil
}

// classifySendFailure separates failures that cannot have delivered a message,
// which retry on their own, from unknown outcomes, which need review because a
// blind retry could send a duplicate. A recipient who refuses messages needs
// no operator action at all.
func classifySendFailure(err error, attempts int) (status, reason string, delaySeconds int) {
	var apiErr telegram.StatusError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case http.StatusTooManyRequests:
			return "pending", "Telegram rate limited message delivery", int(apiErr.RetryDelay().Seconds())
		case http.StatusBadRequest, http.StatusForbidden:
			return "undeliverable", "Telegram refused this recipient", 0
		}
	}
	var requestErr telegram.RequestError
	if errors.As(err, &requestErr) && requestErr.Unsent {
		if attempts+1 >= 8 {
			return "failed", "Telegram could not be reached; retry budget exhausted", 0
		}
		return "pending", "Telegram could not be reached; message not sent", 15 << min(attempts, 8)
	}
	return "uncertain", "Telegram send outcome unknown; review before retrying", 0
}
