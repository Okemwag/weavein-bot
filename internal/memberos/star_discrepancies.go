package memberos

import (
	"net/http"
	"time"
)

func (a *app) adminStarDiscrepancies(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT t.transaction_id,t.direction,t.amount_stars,
        t.raw->'source'->>'invoice_payload',p.id,
        CASE WHEN p.id IS NULL THEN 'no_matching_payment'
             WHEN t.amount_stars<>p.price_stars THEN 'amount_mismatch'
             WHEN t.raw->'source'->>'invoice_payload' IS DISTINCT FROM p.invoice_payload THEN 'payload_mismatch'
             ELSE 'matched' END
        FROM observed_star_transactions t LEFT JOIN payments p ON p.billing_bot_id=t.bot_id
             AND p.telegram_charge_id=t.transaction_id
        WHERE t.bot_id=$1 AND t.direction='incoming'
        AND (p.id IS NULL OR t.amount_stars<>p.price_stars
             OR t.raw->'source'->>'invoice_payload' IS DISTINCT FROM p.invoice_payload)
        ORDER BY t.occurred_at DESC LIMIT 100`, a.cfg.botID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, direction, reason string
		var amount int64
		var payload, payment *string
		if rows.Scan(&id, &direction, &amount, &payload, &payment, &reason) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"transaction_id": id, "direction": direction, "amount_stars": amount, "invoice_payload": payload, "payment_id": payment, "reason": reason})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	rows.Close()
	missing, err := a.db.Query(r.Context(), `SELECT p.telegram_charge_id,p.price_stars,p.id
        FROM payments p LEFT JOIN observed_star_transactions t ON t.bot_id=p.billing_bot_id
            AND t.transaction_id=p.telegram_charge_id AND t.direction='incoming'
        WHERE p.billing_bot_id=$1 AND t.transaction_id IS NULL ORDER BY p.paid_at DESC LIMIT 100`, a.cfg.botID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer missing.Close()
	for missing.Next() {
		var charge, payment string
		var amount int64
		if missing.Scan(&charge, &amount, &payment) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"transaction_id": charge, "direction": "incoming", "amount_stars": amount, "payment_id": payment, "reason": "payment_not_observed"})
	}
	if missing.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	missing.Close()
	refunds, err := a.db.Query(r.Context(), `SELECT t.transaction_id,t.amount_stars,p.id,p.refunded_at
        FROM observed_star_transactions t LEFT JOIN payments p ON p.billing_bot_id=t.bot_id
            AND p.telegram_charge_id=t.transaction_id
        WHERE t.bot_id=$1 AND t.direction='outgoing' AND t.raw->'receiver'->>'type'='user'
        AND (p.id IS NULL OR p.refunded_at IS NULL) ORDER BY t.occurred_at DESC LIMIT 100`, a.cfg.botID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer refunds.Close()
	for refunds.Next() {
		var charge string
		var amount int64
		var payment *string
		var refunded *time.Time
		if refunds.Scan(&charge, &amount, &payment, &refunded) != nil {
			http.Error(w, "database error", 500)
			return
		}
		reason := "refund_without_payment"
		if payment != nil {
			reason = "refund_not_recorded"
		}
		items = append(items, map[string]any{"transaction_id": charge, "direction": "outgoing", "amount_stars": amount, "payment_id": payment, "reason": reason})
	}
	if refunds.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"discrepancies": items, "coverage": "observed bot transactions and recorded payments from completed reconciliation runs"})
}
