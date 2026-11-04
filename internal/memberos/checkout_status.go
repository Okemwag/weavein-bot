package memberos

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) myCheckoutStatus(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	community := r.URL.Query().Get("community_id")
	var id, status string
	var invoiceURL *string
	var created, expires time.Time
	err = a.db.QueryRow(r.Context(), `SELECT ca.id,ca.status,ca.invoice_url,ca.created_at,ca.expires_at
        FROM checkout_attempts ca WHERE ca.community_id::text=$1 AND ca.telegram_user_id=$2
        ORDER BY ca.created_at DESC LIMIT 1`, community, user).Scan(&id, &status, &invoiceURL, &created, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 200, map[string]any{"status": "none"})
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if status == "pending" && expires.Before(time.Now()) {
		status = "expired"
	}
	result := map[string]any{"checkout_id": id, "status": status, "created_at": created, "expires_at": expires}
	if status == "pending" {
		result["invoice_url"] = invoiceURL
	}
	writeJSON(w, 200, result)
}
