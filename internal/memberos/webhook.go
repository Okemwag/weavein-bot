package memberos

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) webhook(w http.ResponseWriter, r *http.Request) {
	botID, err := strconv.ParseInt(r.PathValue("bot_id"), 10, 64)
	if err != nil || botID != a.cfg.botID {
		http.NotFound(w, r)
		return
	}
	supplied := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if subtle.ConstantTimeCompare([]byte(supplied), []byte(a.cfg.webhookSecret)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "update too large", http.StatusRequestEntityTooLarge)
		return
	}
	var update struct {
		ID           int64           `json:"update_id"`
		ChatMember   json.RawMessage `json:"chat_member"`
		Message      json.RawMessage `json:"message"`
		Subscription json.RawMessage `json:"subscription"`
		PreCheckout  json.RawMessage `json:"pre_checkout_query"`
		JoinRequest  json.RawMessage `json:"chat_join_request"`
	}
	if json.Unmarshal(raw, &update) != nil || update.ID <= 0 {
		http.Error(w, "invalid update", 400)
		return
	}
	kind := "unsupported"
	if len(update.ChatMember) > 0 {
		kind = "chat_member"
	} else if len(update.PreCheckout) > 0 {
		kind = "pre_checkout_query"
	} else if len(update.Subscription) > 0 {
		kind = "subscription"
	} else if len(update.JoinRequest) > 0 {
		kind = "chat_join_request"
	} else if len(update.Message) > 0 {
		var message struct {
			Successful json.RawMessage `json:"successful_payment"`
			Refunded   json.RawMessage `json:"refunded_payment"`
			ChatShared json.RawMessage `json:"chat_shared"`
			Text       string          `json:"text"`
			Chat       struct {
				Type string `json:"type"`
			} `json:"chat"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
		}
		if json.Unmarshal(update.Message, &message) == nil {
			if len(message.Successful) > 0 || len(message.Refunded) > 0 {
				kind = "message"
			}
			if len(message.ChatShared) > 0 {
				kind = "chat_shared"
			}
			if botCommand(message.Text, a.cfg.botUsername) != "" {
				kind = "command"
			}
			// Any other private message still proves the bot may reply. Keep only the
			// sender: the inbox has no reason to store what people write to the bot.
			if kind == "unsupported" && message.Chat.Type == "private" && message.From.ID > 0 {
				kind = "contact"
				raw = []byte(`{"update_id":` + strconv.FormatInt(update.ID, 10) + `,"message":{"from":{"id":` + strconv.FormatInt(message.From.ID, 10) + `}}}`)
			}
		}
	}
	if kind == "unsupported" {
		raw = []byte(`{"update_id":` + strconv.FormatInt(update.ID, 10) + `}`)
	}
	state := "pending"
	if kind == "pre_checkout_query" {
		state = "awaiting_answer"
	}
	requestCtx := r.Context()
	if kind == "pre_checkout_query" {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(requestCtx, 9*time.Second)
		defer cancel()
	}
	_, err = a.db.Exec(requestCtx, `INSERT INTO telegram_updates(bot_id,update_id,update_type,payload,state)
        VALUES($1,$2,$3,$4,$5) ON CONFLICT (bot_id,update_id) DO NOTHING`, botID, update.ID, kind, raw, state)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if kind == "pre_checkout_query" {
		var claimed int64
		err := a.db.QueryRow(requestCtx, `UPDATE telegram_updates SET state='answering'
			WHERE bot_id=$1 AND update_id=$2 AND state='awaiting_answer' RETURNING update_id`,
			botID, update.ID).Scan(&claimed)
		if err == pgx.ErrNoRows {
			writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
			return
		}
		if err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := a.answerPreCheckout(requestCtx, botID, update.ID, update.PreCheckout); err != nil {
			markCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = a.db.Exec(markCtx, `UPDATE telegram_updates SET state='answer_unknown',
				last_error='Telegram pre-checkout answer unconfirmed',processed_at=now()
				WHERE bot_id=$1 AND update_id=$2 AND state='answering'`, botID, update.ID)
			cancel()
			http.Error(w, "pre-checkout response failed", http.StatusServiceUnavailable)
			return
		}
	}
	writeJSON(w, 200, map[string]string{"status": "accepted"})
}
