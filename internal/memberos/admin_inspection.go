package memberos

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
)

func (a *app) adminCreators(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT cr.id,cr.display_name,w.id,w.name,c.id,c.title,c.billing_mode,c.status,c.connection_status,w.deletion_requested_at
 FROM creators cr JOIN workspaces w ON w.creator_id=cr.id LEFT JOIN communities c ON c.workspace_id=w.id
 ORDER BY cr.created_at DESC,c.created_at DESC LIMIT 200`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var creator, name, workspace, workspaceName string
		var community, title, mode, status, health *string
		var deletionRequested *time.Time
		if rows.Scan(&creator, &name, &workspace, &workspaceName, &community, &title, &mode, &status, &health, &deletionRequested) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"creator_id": creator, "creator_name": name, "workspace_id": workspace, "workspace_name": workspaceName,
			"community_id": community, "community_title": title, "billing_mode": mode, "status": status, "connection_status": health,
			"deletion_requested_at": deletionRequested})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"creators": items, "limit": 200})
}

func (a *app) adminIncidents(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 80 {
		http.Error(w, "search too long", http.StatusBadRequest)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT s.id,s.telegram_user_id,s.state,s.paid_through,c.id,c.title,
  p.telegram_charge_id,
  (SELECT count(*) FROM access_jobs j WHERE j.community_id=c.id AND j.telegram_user_id=s.telegram_user_id AND j.status='failed')
 FROM subscriptions s JOIN communities c ON c.id=s.community_id
 LEFT JOIN LATERAL (SELECT telegram_charge_id FROM payments WHERE subscription_id=s.id ORDER BY paid_at DESC LIMIT 1) p ON true
 WHERE $1='' OR s.telegram_user_id::text LIKE '%'||$1||'%' OR c.title ILIKE '%'||$1||'%'
  OR p.telegram_charge_id=$1 OR s.id::text=$1
 ORDER BY s.updated_at DESC LIMIT 100`, q)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var sub, state, community, title string
		var user int64
		var paid time.Time
		var charge *string
		var failures int
		if rows.Scan(&sub, &user, &state, &paid, &community, &title, &charge, &failures) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"subscription_id": sub, "telegram_user_id": user, "state": state, "paid_through": paid,
			"community_id": community, "community_title": title, "last_charge_id": charge, "failed_access_jobs": failures})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"incidents": items, "coverage": "known managed subscriptions only"})
}

func (a *app) adminHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var pendingUpdates, failedUpdates, pendingJobs, failedJobs, failedNotices, openTickets int
	err := a.db.QueryRow(r.Context(), `SELECT
  (SELECT count(*) FROM telegram_updates WHERE state='pending'),
  (SELECT count(*) FROM telegram_updates WHERE state IN ('failed','quarantined')),
  (SELECT count(*) FROM access_jobs WHERE status='pending'),
  (SELECT count(*) FROM access_jobs WHERE status='failed'),
  (SELECT count(*) FROM notifications WHERE status='failed'),
  (SELECT count(*) FROM support_tickets WHERE status<>'resolved')`).
		Scan(&pendingUpdates, &failedUpdates, &pendingJobs, &failedJobs, &failedNotices, &openTickets)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,title,connection_status,last_permission_check_at,last_permission_error
 FROM communities WHERE billing_mode='managed_bot' AND connection_status<>'healthy'
 ORDER BY last_permission_check_at NULLS FIRST LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	communities := make([]map[string]any, 0)
	for rows.Next() {
		var id, title, status string
		var checked *time.Time
		var reason *string
		if rows.Scan(&id, &title, &status, &checked, &reason) != nil {
			http.Error(w, "database error", 500)
			return
		}
		communities = append(communities, map[string]any{"id": id, "title": title, "status": status, "last_checked_at": checked, "error": reason})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"pending_updates": pendingUpdates, "failed_updates": failedUpdates, "pending_access_jobs": pendingJobs,
		"failed_access_jobs": failedJobs, "failed_notifications": failedNotices, "open_support_tickets": openTickets,
		"unhealthy_communities": communities, "telegram_webhook": a.webhookHealth(r.Context())})
}

func (a *app) adminAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,creator_id,subject_telegram_user_id,action,happened_at FROM audit_events
 ORDER BY happened_at DESC LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, action string
		var creator *string
		var subject *int64
		var at time.Time
		if rows.Scan(&id, &creator, &subject, &action, &at) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "creator_id": creator, "subject_telegram_user_id": subject, "action": action, "at": at})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"audit": items, "details": "redacted"})
}

func (a *app) adminSupportTickets(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT t.id,t.telegram_user_id,t.kind,t.description,t.status,t.created_at,
		t.escalated_at,t.assigned_operator_id,t.escalation_reason,
 c.id,c.title,cr.support_contact FROM support_tickets t JOIN communities c ON c.id=t.community_id
 JOIN creators cr ON cr.id=c.creator_id WHERE t.status<>'resolved' ORDER BY t.created_at LIMIT 100`)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, kind, description, status, community, title, contact string
		var user int64
		var at time.Time
		var escalatedAt *time.Time
		var assigned *int64
		var reason *string
		if rows.Scan(&id, &user, &kind, &description, &status, &at, &escalatedAt, &assigned, &reason,
			&community, &title, &contact) != nil {
			http.Error(w, "database error", 500)
			return
		}
		items = append(items, map[string]any{"id": id, "telegram_user_id": user, "kind": kind, "description": description, "status": status,
			"created_at": at, "escalation_due": escalatedAt == nil && at.Before(time.Now().Add(-48*time.Hour)),
			"escalated_at": escalatedAt, "assigned_operator_id": assigned, "escalation_reason": reason,
			"community_id": community, "community_title": title, "support_contact": contact})
	}
	if rows.Err() != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"tickets": items, "escalation_policy": "open after 48 hours"})
}

func (a *app) adminResolveSupportTicket(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var input struct {
		Resolution string `json:"resolution"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil || len(strings.TrimSpace(input.Resolution)) < 10 || len(input.Resolution) > 1000 {
		http.Error(w, "invalid resolution", http.StatusBadRequest)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var community string
	var member int64
	err = tx.QueryRow(r.Context(), `UPDATE support_tickets SET status='resolved',resolution=$2,updated_at=now()
 WHERE id::text=$1 AND status<>'resolved' RETURNING community_id,telegram_user_id`, r.PathValue("id"), strings.TrimSpace(input.Resolution)).Scan(&community, &member)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
 VALUES($1,'platform_support_resolved',jsonb_build_object('ticket_id',$2,'community_id',$3,'member_id',$4))`, admin, r.PathValue("id"), community, member)
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "resolved"})
}
