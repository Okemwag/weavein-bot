package memberos

import (
	"encoding/json"
	"memberos/internal/telegram"
	"memberos/web/miniapp"
	"memberos/web/publicsite"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type app struct {
	cfg      config
	db       *pgxpool.Pool
	telegram telegram.Client
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", publicsite.Handler(a.cfg.botUsername, a.cfg.policies))
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
	handle(mux, "POST /api/v1/telegram/session", a.session)
	handle(mux, "POST /api/v1/creator-inquiries", a.submitCreatorInquiry)
	handle(mux, "GET /api/v1/communities/{id}", a.community)
	handle(mux, "GET /api/v1/communities", a.listCommunities)
	handle(mux, "GET /api/v1/me/membership", a.membership)
	handle(mux, "GET /api/v1/me/memberships", a.myMemberships)
	handle(mux, "POST /api/v1/me/subscriptions/{id}/cancel", a.cancelSubscription)
	handle(mux, "GET /api/v1/creator/communities", a.creatorCommunities)
	handle(mux, "POST /api/v1/creator/workspaces/{id}/chat-selection", a.prepareChatSelection)
	handle(mux, "GET /api/v1/creator/workspaces/{id}/chat-selection", a.selectedChat)
	handle(mux, "POST /api/v1/creator/workspaces/{id}/communities", a.connectCommunity)
	handle(mux, "POST /api/v1/creator/communities/{id}/recheck", a.recheckCommunity)
	handle(mux, "PATCH /api/v1/creator/communities/{id}/onboarding", a.updateCommunityOnboarding)
	handle(mux, "GET /api/v1/creator/communities/{id}/onboarding", a.creatorCommunityOnboarding)
	handle(mux, "POST /api/v1/creator/communities/{id}/disconnect", a.disconnectCommunity)
	handle(mux, "GET /api/v1/creator/workspaces", a.workspaces)
	handle(mux, "POST /api/v1/creator/workspaces", a.workspaces)
	handle(mux, "PATCH /api/v1/creator/workspaces/{id}", a.updateWorkspace)
	handle(mux, "PATCH /api/v1/creator/workspaces/{id}/settings", a.updateWorkspaceSettings)
	handle(mux, "POST /api/v1/creator/workspaces/{id}/deletion-request", a.requestWorkspaceDeletion)
	handle(mux, "GET /api/v1/creator/workspaces/{id}/onboarding", a.onboarding)
	handle(mux, "GET /api/v1/creator/workspaces/{id}/saas", a.creatorSaaSAccount)
	handle(mux, "GET /api/v1/communities/{id}/plans", a.plans)
	handle(mux, "GET /api/v1/creator/communities/{id}/plans", a.creatorPlans)
	handle(mux, "POST /api/v1/creator/communities/{id}/plans", a.plans)
	handle(mux, "POST /api/v1/creator/plans/{id}/versions", a.revisePlan)
	handle(mux, "PATCH /api/v1/creator/plans/{id}", a.setPlanStatus)
	handle(mux, "GET /api/v1/creator/communities/{id}/overview", a.overview)
	handle(mux, "GET /api/v1/creator/communities/{id}/members", a.creatorMembers)
	handle(mux, "GET /api/v1/creator/communities/{id}/members.csv", a.creatorMembersCSV)
	handle(mux, "GET /api/v1/creator/communities/{id}/members/{user_id}", a.creatorMemberProfile)
	handle(mux, "POST /api/v1/creator/communities/{id}/members/{user_id}/notes", a.creatorMemberNote)
	handle(mux, "GET /api/v1/creator/communities/{id}/access-overrides", a.creatorAccessOverrides)
	handle(mux, "POST /api/v1/creator/communities/{id}/access-overrides", a.creatorGrantOverride)
	handle(mux, "POST /api/v1/creator/communities/{id}/access-overrides/{override_id}/revoke", a.creatorRevokeOverride)
	handle(mux, "GET /api/v1/creator/communities/{id}/access-mismatches", a.creatorAccessMismatches)
	handle(mux, "GET /api/v1/creator/communities/{id}/payments", a.creatorPayments)
	handle(mux, "GET /api/v1/creator/communities/{id}/payments.csv", a.creatorPaymentsCSV)
	handle(mux, "GET /api/v1/creator/communities/{id}/support-tickets", a.creatorSupportTickets)
	handle(mux, "POST /api/v1/creator/communities/{id}/support-tickets/{ticket_id}/resolve", a.creatorResolveTicket)
	handle(mux, "POST /api/v1/me/support-tickets", a.createSupportTicket)
	handle(mux, "POST /api/v1/me/refund-requests", a.createRefundRequest)
	handle(mux, "GET /api/v1/me/payments", a.myPayments)
	handle(mux, "GET /api/v1/me/checkout-status", a.myCheckoutStatus)
	handle(mux, "GET /api/v1/me/notification-preferences", a.notificationPreferences)
	handle(mux, "PATCH /api/v1/me/notification-preferences", a.notificationPreferences)
	handle(mux, "GET /api/v1/admin/updates", a.adminUpdates)
	handle(mux, "POST /api/v1/admin/updates/{bot_id}/{update_id}/retry", a.adminRetryUpdate)
	handle(mux, "GET /api/v1/admin/access-jobs", a.adminAccessJobs)
	handle(mux, "POST /api/v1/admin/access-jobs/{id}/retry", a.adminRetryAccessJob)
	handle(mux, "GET /api/v1/admin/notifications", a.adminNotifications)
	handle(mux, "POST /api/v1/admin/notifications/{id}/retry", a.adminRetryNotification)
	handle(mux, "POST /api/v1/admin/updates/{bot_id}/{update_id}/acknowledge", a.adminAcknowledgeUpdate)
	handle(mux, "POST /api/v1/admin/access-jobs/{id}/acknowledge", a.adminAcknowledgeAccessJob)
	handle(mux, "POST /api/v1/admin/notifications/retry-uncertain", a.adminRetryUncertainNotifications)
	handle(mux, "POST /api/v1/admin/notifications/{id}/acknowledge", a.adminAcknowledgeNotification)
	handle(mux, "POST /api/v1/admin/refund-requests/{id}/reject", a.adminRejectRefund)
	handle(mux, "POST /api/v1/admin/refund-requests/{id}/reopen", a.adminReopenRefund)
	handle(mux, "POST /api/v1/admin/reconcile-access", a.adminReconcileAccess)
	handle(mux, "POST /api/v1/admin/reconcile-stars", a.adminReconcileStars)
	handle(mux, "GET /api/v1/admin/star-discrepancies", a.adminStarDiscrepancies)
	handle(mux, "GET /api/v1/admin/refund-requests", a.adminRefundRequests)
	handle(mux, "POST /api/v1/admin/refund-requests/{id}/approve", a.adminApproveRefund)
	handle(mux, "GET /api/v1/admin/creators", a.adminCreators)
	handle(mux, "GET /api/v1/admin/incidents", a.adminIncidents)
	handle(mux, "GET /api/v1/admin/health", a.adminHealth)
	handle(mux, "GET /api/v1/admin/audit", a.adminAudit)
	handle(mux, "GET /api/v1/admin/support-tickets", a.adminSupportTickets)
	handle(mux, "GET /api/v1/admin/creator-inquiries", a.adminCreatorInquiries)
	handle(mux, "POST /api/v1/admin/creator-inquiries/{id}/resolve", a.adminResolveCreatorInquiry)
	handle(mux, "PATCH /api/v1/admin/workspaces/{id}/saas", a.adminSetSaaSAccount)
	handle(mux, "GET /api/v1/admin/saas-finance", a.adminSaaSFinance)
	handle(mux, "POST /api/v1/admin/support-tickets/{id}/resolve", a.adminResolveSupportTicket)
	handle(mux, "POST /api/v1/admin/support-tickets/{id}/escalate", a.adminEscalateSupportTicket)
	handle(mux, "GET /api/v1/admin/support-tickets/{id}/evidence", a.adminDisputeEvidence)
	handle(mux, "POST /api/v1/telegram/webhooks/{bot_id}", a.webhook)
	handle(mux, "POST /api/v1/checkouts", a.checkout)
	return a.withHTTPMiddleware(mux)
}

// handle registers a route and rejects malformed UUID path values with 404, so
// handlers can compare them directly against indexed uuid columns.
func handle(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	var ids []string
	for _, name := range []string{"id", "override_id", "ticket_id"} {
		if strings.Contains(pattern, "{"+name+"}") {
			ids = append(ids, name)
		}
	}
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		for _, name := range ids {
			if !validUUID(r.PathValue(name)) {
				http.NotFound(w, r)
				return
			}
		}
		handler(w, r)
	})
}

// validUUID accepts the canonical 8-4-4-4-12 form that Postgres returns.
func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F'):
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
