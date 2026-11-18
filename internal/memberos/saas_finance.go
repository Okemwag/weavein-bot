package memberos

import "net/http"

// SaaS money is reported only from the dedicated SaaS ledger. Creator Stars
// payments never contribute to these amounts.
func (a *app) adminSaaSFinance(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT currency,
		coalesce(sum(CASE WHEN kind='charge' THEN amount_minor ELSE 0 END),0),
		coalesce(sum(CASE WHEN kind='refund' THEN amount_minor ELSE 0 END),0),count(*)
		FROM saas_financial_events GROUP BY currency ORDER BY currency`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var currency string
		var charged, refunded, count int64
		if rows.Scan(&currency, &charged, &refunded, &count) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"currency": currency, "charged_minor": charged,
			"refunded_minor": refunded, "net_minor": charged - refunded, "events": count})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"saas_revenue": items, "coverage": "Dedicated MemberOS SaaS ledger only: MemberOS Pro Stars charges and refunds. Creator community payments are never included."})
}
