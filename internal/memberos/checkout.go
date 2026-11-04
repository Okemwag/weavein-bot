package memberos

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
)

func randomPayload() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func (a *app) checkout(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !a.cfg.managedEnabled || a.cfg.botRecipientAck != strconv.FormatInt(a.cfg.botID, 10) {
		http.Error(w, "managed checkout unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		CommunityID string `json:"community_id"`
		PlanID      string `json:"plan_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || body.CommunityID == "" {
		http.Error(w, "invalid request", 400)
		return
	}
	var communityID, creatorID, title, planVersionID, planName string
	var price int
	err = a.db.QueryRow(r.Context(), `SELECT c.id,c.creator_id,c.title,v.id,p.name,v.price_stars FROM communities c
		JOIN saas_accounts sa ON sa.workspace_id=c.workspace_id AND sa.status='active' AND sa.managed_billing_allowed
		JOIN plans p ON p.community_id=c.id AND p.workspace_id=c.workspace_id AND p.status='active'
		JOIN LATERAL (SELECT * FROM plan_versions WHERE plan_id=p.id ORDER BY version DESC LIMIT 1) v ON true
		WHERE c.id::text=$1 AND c.status='published' AND c.billing_mode='managed_bot' AND c.billing_bot_id=$2
		AND (p.id::text=$3 OR ($3='' AND p.name='Membership'))`, body.CommunityID, a.cfg.botID, body.PlanID).
		Scan(&communityID, &creatorID, &title, &planVersionID, &planName, &price)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	payload, err := randomPayload()
	if err != nil {
		http.Error(w, "checkout unavailable", http.StatusServiceUnavailable)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	// A disconnect takes an exclusive lock on this row. Recheck publication
	// under a shared lock so checkout cannot race the final disconnect check.
	var checkoutAllowed bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM communities c JOIN saas_accounts sa ON sa.workspace_id=c.workspace_id
		WHERE c.id=$1 AND c.status='published' AND c.billing_mode='managed_bot'
		AND c.billing_bot_id=$2 AND sa.status='active' AND sa.managed_billing_allowed FOR SHARE OF c,sa)`, communityID, a.cfg.botID).Scan(&checkoutAllowed); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if !checkoutAllowed {
		http.Error(w, "community is no longer accepting checkout", http.StatusConflict)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO members(creator_id,telegram_user_id,first_name,username)
        VALUES($1,$2,coalesce((SELECT first_name FROM telegram_profiles WHERE telegram_user_id=$2),''),
        coalesce((SELECT username FROM telegram_profiles WHERE telegram_user_id=$2),''))
        ON CONFLICT (creator_id,telegram_user_id) DO UPDATE SET first_name=EXCLUDED.first_name,
        username=EXCLUDED.username,verified_at=now()`, creatorID, userID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var checkoutID string
	err = tx.QueryRow(r.Context(), `INSERT INTO checkout_attempts(community_id,creator_id,telegram_user_id,billing_bot_id,
		invoice_payload,price_stars,plan_version_id,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,now()+interval '30 minutes') RETURNING id`,
		communityID, creatorID, userID, a.cfg.botID, payload, price, planVersionID).Scan(&checkoutID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if len([]rune(title)) > 32 {
		title = string([]rune(title)[:32])
	}
	prices, _ := json.Marshal([]map[string]any{{"label": planName, "amount": price}})
	var invoiceURL string
	err = a.telegram.Call(r.Context(), "createInvoiceLink", url.Values{
		"title": {title}, "description": {"30-day community membership"}, "payload": {payload},
		"currency": {"XTR"}, "prices": {string(prices)}, "subscription_period": {"2592000"},
	}, &invoiceURL)
	if err != nil || invoiceURL == "" {
		_, _ = a.db.Exec(context.Background(), `UPDATE checkout_attempts SET status='failed' WHERE id=$1 AND status='pending'`, checkoutID)
		http.Error(w, "Telegram invoice unavailable", http.StatusBadGateway)
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE checkout_attempts SET invoice_url=$2 WHERE id=$1`, checkoutID, invoiceURL); err != nil {
		http.Error(w, "checkout storage unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, 201, map[string]any{"checkout_id": checkoutID, "plan_version_id": planVersionID, "invoice_url": invoiceURL, "currency": "XTR", "price_stars": price, "expires_in": 1800})
}
