package memberos

import (
	"encoding/json"
	"memberos/internal/telegram"
	"memberos/web/miniapp"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type app struct {
	cfg      config
	db       *pgxpool.Pool
	telegram telegram.Client
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusTemporaryRedirect)
	})
	mux.Handle("GET /app/", http.StripPrefix("/app/", miniapp.Handler()))
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := a.db.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/v1/telegram/session", a.session)
	mux.HandleFunc("GET /api/v1/communities/{id}", a.community)
	mux.HandleFunc("GET /api/v1/communities", a.listCommunities)
	mux.HandleFunc("GET /api/v1/me/membership", a.membership)
	mux.HandleFunc("POST /api/v1/me/subscriptions/{id}/cancel", a.cancelSubscription)
	mux.HandleFunc("GET /api/v1/creator/communities", a.creatorCommunities)
	mux.HandleFunc("POST /api/v1/creator/workspaces/{id}/communities", a.connectCommunity)
	mux.HandleFunc("POST /api/v1/creator/communities/{id}/recheck", a.recheckCommunity)
	mux.HandleFunc("GET /api/v1/creator/workspaces", a.workspaces)
	mux.HandleFunc("POST /api/v1/creator/workspaces", a.workspaces)
	mux.HandleFunc("PATCH /api/v1/creator/workspaces/{id}", a.updateWorkspace)
	mux.HandleFunc("GET /api/v1/creator/workspaces/{id}/onboarding", a.onboarding)
	mux.HandleFunc("GET /api/v1/communities/{id}/plans", a.plans)
	mux.HandleFunc("GET /api/v1/creator/communities/{id}/plans", a.creatorPlans)
	mux.HandleFunc("POST /api/v1/creator/communities/{id}/plans", a.plans)
	mux.HandleFunc("POST /api/v1/creator/plans/{id}/versions", a.revisePlan)
	mux.HandleFunc("PATCH /api/v1/creator/plans/{id}", a.setPlanStatus)
	mux.HandleFunc("GET /api/v1/creator/communities/{id}/overview", a.overview)
	mux.HandleFunc("GET /api/v1/creator/communities/{id}/members", a.creatorMembers)
	mux.HandleFunc("GET /api/v1/creator/communities/{id}/access-mismatches", a.creatorAccessMismatches)
	mux.HandleFunc("GET /api/v1/creator/communities/{id}/payments", a.creatorPayments)
	mux.HandleFunc("GET /api/v1/creator/communities/{id}/support-tickets", a.creatorSupportTickets)
	mux.HandleFunc("POST /api/v1/me/support-tickets", a.createSupportTicket)
	mux.HandleFunc("POST /api/v1/me/refund-requests", a.createRefundRequest)
	mux.HandleFunc("GET /api/v1/admin/updates", a.adminUpdates)
	mux.HandleFunc("POST /api/v1/admin/updates/{bot_id}/{update_id}/retry", a.adminRetryUpdate)
	mux.HandleFunc("GET /api/v1/admin/access-jobs", a.adminAccessJobs)
	mux.HandleFunc("POST /api/v1/admin/access-jobs/{id}/retry", a.adminRetryAccessJob)
	mux.HandleFunc("POST /api/v1/admin/reconcile-access", a.adminReconcileAccess)
	mux.HandleFunc("POST /api/v1/admin/reconcile-stars", a.adminReconcileStars)
	mux.HandleFunc("GET /api/v1/admin/refund-requests", a.adminRefundRequests)
	mux.HandleFunc("POST /api/v1/admin/refund-requests/{id}/approve", a.adminApproveRefund)
	mux.HandleFunc("POST /api/v1/telegram/webhooks/{bot_id}", a.webhook)
	mux.HandleFunc("POST /api/v1/checkouts", a.checkout)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
