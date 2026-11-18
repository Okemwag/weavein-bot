package billing

import (
	"context"
	"encoding/json"
	"memberos/internal/eventing"

	"github.com/jackc/pgx/v5"
)

type paymentMessage struct {
	Date int64 `json:"date"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Successful *struct {
		Currency   string `json:"currency"`
		Amount     int    `json:"total_amount"`
		Payload    string `json:"invoice_payload"`
		Expiration int64  `json:"subscription_expiration_date"`
		Recurring  bool   `json:"is_recurring"`
		First      bool   `json:"is_first_recurring"`
		ChargeID   string `json:"telegram_payment_charge_id"`
	} `json:"successful_payment"`
	Refunded *struct {
		Currency string `json:"currency"`
		Amount   int    `json:"total_amount"`
		Payload  string `json:"invoice_payload"`
		ChargeID string `json:"telegram_payment_charge_id"`
	} `json:"refunded_payment"`
}

type subscriptionChange struct {
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
	Payload string `json:"invoice_payload"`
	State   string `json:"state"`
}

// ProcessMessage takes seq, the inbox position. Telegram update IDs cannot order
// events because they turn random after a quiet week.
func ProcessMessage(ctx context.Context, tx pgx.Tx, botID, updateID, seq int64, raw []byte) error {
	var wrapper struct {
		Message paymentMessage `json:"message"`
	}
	if json.Unmarshal(raw, &wrapper) != nil {
		return eventing.InvalidEvent("invalid message JSON")
	}
	message := wrapper.Message
	if message.Successful != nil {
		if IsSaaSPayload(message.Successful.Payload) {
			return recordSaaSPayment(ctx, tx, botID, updateID, seq, message)
		}
		return recordPayment(ctx, tx, botID, updateID, seq, message)
	}
	if message.Refunded != nil {
		if IsSaaSPayload(message.Refunded.Payload) {
			return recordSaaSRefund(ctx, tx, botID, updateID, message)
		}
		return recordRefund(ctx, tx, botID, updateID, seq, message)
	}
	return eventing.IgnoredEvent("non-payment message ignored")
}
