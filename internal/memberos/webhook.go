package memberos

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
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
		}
		if json.Unmarshal(update.Message, &message) == nil && (len(message.Successful) > 0 || len(message.Refunded) > 0) {
			kind = "message"
		}
	}
	if kind == "unsupported" {
		raw = []byte(`{"update_id":` + strconv.FormatInt(update.ID, 10) + `}`)
	}
	state := "pending"
	if kind == "pre_checkout_query" {
		state = "awaiting_answer"
	}
	_, err = a.db.Exec(r.Context(), `INSERT INTO telegram_updates(bot_id,update_id,update_type,payload,state)
        VALUES($1,$2,$3,$4,$5) ON CONFLICT (bot_id,update_id) DO NOTHING`, botID, update.ID, kind, raw, state)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if kind == "pre_checkout_query" {
		var currentState string
		if err := a.db.QueryRow(r.Context(), `SELECT state FROM telegram_updates WHERE bot_id=$1 AND update_id=$2`, botID, update.ID).Scan(&currentState); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		if currentState == "processed" {
			writeJSON(w, 200, map[string]string{"status": "accepted"})
			return
		}
		if err := a.answerPreCheckout(r.Context(), botID, update.ID, update.PreCheckout); err != nil {
			http.Error(w, "pre-checkout response failed", http.StatusServiceUnavailable)
			return
		}
	}
	writeJSON(w, 200, map[string]string{"status": "accepted"})
}
