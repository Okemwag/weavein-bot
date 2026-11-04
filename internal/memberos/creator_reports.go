package memberos

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"
)

func (a *app) creatorPaymentsCSV(w http.ResponseWriter, r *http.Request) {
	community, ok := a.creatorManagedCommunity(w, r)
	if !ok {
		return
	}
	period, err := parseReportPeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT s.telegram_user_id,p.telegram_charge_id,p.price_stars,
        p.paid_at,p.provider_expiration,p.is_first_recurring,p.refunded_at
        FROM payments p JOIN subscriptions s ON s.id=p.subscription_id
        WHERE s.community_id=$1 AND p.paid_at>=$2 AND p.paid_at<$3
        ORDER BY p.paid_at DESC LIMIT 10000`, community, period.From, period.To)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=memberos-payments.csv")
	w.Header().Set("Cache-Control", "no-store")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"telegram_user_id", "telegram_charge_id", "price_stars", "paid_at", "paid_through", "first_payment", "refunded_at"})
	for rows.Next() {
		var user int64
		var charge string
		var stars int
		var paid, until time.Time
		var first bool
		var refunded *time.Time
		if rows.Scan(&user, &charge, &stars, &paid, &until, &first, &refunded) != nil {
			return
		}
		refundDate := ""
		if refunded != nil {
			refundDate = refunded.UTC().Format(time.RFC3339)
		}
		_ = writer.Write([]string{strconv.FormatInt(user, 10), csvSafe(charge), strconv.Itoa(stars), paid.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339), strconv.FormatBool(first), refundDate})
	}
	writer.Flush()
}
