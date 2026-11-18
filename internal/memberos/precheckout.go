package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
)

type preCheckoutQuery struct {
	ID   string `json:"id"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Currency    string `json:"currency"`
	TotalAmount int    `json:"total_amount"`
	Payload     string `json:"invoice_payload"`
}

func (a *app) answerPreCheckout(ctx context.Context, botID, updateID int64, raw json.RawMessage) error {
	var query preCheckoutQuery
	if err := json.Unmarshal(raw, &query); err != nil || query.ID == "" {
		return errors.New("invalid pre-checkout query")
	}
	var valid bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM checkout_attempts ca JOIN communities c ON c.id=ca.community_id
		JOIN saas_accounts sa ON sa.workspace_id=c.workspace_id AND sa.status='active' AND sa.managed_billing_allowed
        WHERE ca.invoice_payload=$1 AND ca.telegram_user_id=$2 AND ca.billing_bot_id=$3
        AND ca.price_stars=$4 AND ca.status='pending' AND ca.expires_at>now()
        AND c.status='published' AND c.billing_mode='managed_bot' AND c.billing_bot_id=$3
        AND NOT member_has_live_subscription(ca.community_id,ca.telegram_user_id))`,
		query.Payload, query.From.ID, botID, query.TotalAmount).Scan(&valid)
	if err != nil {
		return err
	}
	valid = valid && query.Currency == "XTR" && query.From.ID > 0
	params := url.Values{"pre_checkout_query_id": {query.ID}, "ok": {strconv.FormatBool(valid)}}
	if !valid {
		params.Set("error_message", "This invoice has expired or you are already a member. Please return to MemberOS.")
	}
	var accepted bool
	if err := a.telegram.Call(ctx, "answerPreCheckoutQuery", params, &accepted); err != nil {
		return err
	}
	if !accepted {
		return errors.New("telegram did not confirm pre-checkout answer")
	}
	_, err = a.db.Exec(ctx, `UPDATE telegram_updates SET state='processed',processed_at=now()
		WHERE bot_id=$1 AND update_id=$2 AND state='answering'`, botID, updateID)
	return err
}
