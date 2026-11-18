package memberos

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (a *app) adminReconcileStars(w http.ResponseWriter, r *http.Request) {
	operatorID, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var runID string
	if err := a.db.QueryRow(r.Context(), `INSERT INTO star_reconciliation_runs(bot_id,status,operator_telegram_user_id)
		VALUES($1,'running',$2) RETURNING id`, a.cfg.botID, operatorID).Scan(&runID); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if _, err := a.db.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,'stars_reconciliation_started',jsonb_build_object('run_id',$2::uuid,'bot_id',$3::bigint))`, operatorID, runID, a.cfg.botID); err != nil {
		a.finishStarRun(r, runID, "failed", 0, 0, "audit write failed")
		http.Error(w, "database error", 500)
		return
	}
	var balance struct {
		Amount         int64 `json:"amount"`
		NanostarAmount int64 `json:"nanostar_amount"`
	}
	if err := a.telegram.Call(r.Context(), "getMyStarBalance", nil, &balance); err != nil {
		a.finishStarRun(r, runID, "failed", 0, 0, "balance lookup failed")
		http.Error(w, "Telegram balance unavailable", http.StatusBadGateway)
		return
	}
	count := 0
	for offset := 0; ; offset += 100 {
		var page struct {
			Transactions []json.RawMessage `json:"transactions"`
		}
		if err := a.telegram.Call(r.Context(), "getStarTransactions", url.Values{"offset": {strconv.Itoa(offset)}, "limit": {"100"}}, &page); err != nil {
			a.finishStarRun(r, runID, "failed", balance.Amount, count, "transaction lookup failed")
			http.Error(w, "Telegram transactions unavailable", http.StatusBadGateway)
			return
		}
		for _, raw := range page.Transactions {
			var item struct {
				ID       string          `json:"id"`
				Amount   int64           `json:"amount"`
				Date     int64           `json:"date"`
				Source   json.RawMessage `json:"source"`
				Receiver json.RawMessage `json:"receiver"`
			}
			if json.Unmarshal(raw, &item) != nil || item.ID == "" || item.Date <= 0 {
				continue
			}
			direction := "incoming"
			if len(item.Receiver) > 0 {
				direction = "outgoing"
			}
			_, err := a.db.Exec(r.Context(), `INSERT INTO observed_star_transactions(bot_id,transaction_id,direction,amount_stars,occurred_at,raw)
                VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (bot_id,transaction_id,direction) DO UPDATE
                SET raw=EXCLUDED.raw,observed_at=now()`, a.cfg.botID, item.ID, direction, item.Amount, time.Unix(item.Date, 0).UTC(), raw)
			if err != nil {
				a.finishStarRun(r, runID, "failed", balance.Amount, count, "database write failed")
				http.Error(w, "database error", 500)
				return
			}
			count++
		}
		if len(page.Transactions) < 100 {
			break
		}
	}
	a.finishStarRun(r, runID, "complete", balance.Amount, count, "")
	writeJSON(w, 200, map[string]any{"run_id": runID, "status": "complete", "observed_transactions": count,
		"balance_stars": balance.Amount, "balance_nanostars": balance.NanostarAmount,
		"coverage": "all pages returned by the bot transaction API during this run; Telegram charge IDs matched by transaction ID and direction"})
}

func (a *app) finishStarRun(r *http.Request, id, status string, balance int64, count int, reason string) {
	_, _ = a.db.Exec(r.Context(), `UPDATE star_reconciliation_runs SET status=$2,balance_stars=$3,transaction_count=$4,
        error=NULLIF($5,''),finished_at=now() WHERE id=$1`, id, status, balance, count, reason)
}
