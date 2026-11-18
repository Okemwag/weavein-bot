package memberos

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) creatorSaaSAccount(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var workspace, plan, status string
	var limit, used int
	var managed bool
	var updated time.Time
	var proThrough, lapsedAt *time.Time
	err = a.db.QueryRow(r.Context(), `SELECT sa.workspace_id,sa.plan_code,sa.status,
		sa.community_limit,sa.managed_billing_allowed,sa.updated_at,sa.pro_paid_through,sa.lapsed_at,
		(SELECT count(*) FROM communities c WHERE c.workspace_id=sa.workspace_id AND c.status<>'archived')
		FROM saas_accounts sa JOIN workspace_members wm ON wm.workspace_id=sa.workspace_id
		WHERE sa.workspace_id=$1 AND wm.telegram_user_id=$2`, r.PathValue("id"), user).
		Scan(&workspace, &plan, &status, &limit, &managed, &updated, &proThrough, &lapsedAt, &used)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	response := map[string]any{"workspace_id": workspace, "plan": plan, "status": status,
		"community_limit": limit, "communities_used": used, "managed_billing_allowed": managed,
		"updated_at": updated, "pro_paid_through": proThrough, "lapsed_at": lapsedAt,
		"pro_offer": map[string]any{"available": a.cfg.saasEnabled && plan != "pilot", "price_stars": a.cfg.saasProPrice,
			"community_limit": a.cfg.saasProLimit, "period_days": 30, "grace_days": saasGraceDays}}
	var subState string
	var subThrough time.Time
	var payer int64
	err = a.db.QueryRow(r.Context(), `SELECT state,paid_through,telegram_user_id FROM saas_subscriptions WHERE workspace_id=$1
		AND state IN ('active','cancel_scheduled','payment_failed') AND paid_through>now()-make_interval(days => $2)
		ORDER BY paid_through DESC LIMIT 1`, workspace, saasGraceDays).Scan(&subState, &subThrough, &payer)
	if err == nil {
		response["subscription"] = map[string]any{"state": subState, "paid_through": subThrough, "you_paid": payer == user}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, response)
}

func (a *app) adminSetSaaSAccount(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var input struct {
		Plan                  string `json:"plan"`
		Status                string `json:"status"`
		CommunityLimit        int    `json:"community_limit"`
		ManagedBillingAllowed bool   `json:"managed_billing_allowed"`
		Reason                string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if (input.Plan != "free" && input.Plan != "pilot") || (input.Status != "active" && input.Status != "suspended") ||
		input.CommunityLimit < 1 || input.CommunityLimit > 1000 || len(input.Reason) < 10 || len(input.Reason) > 500 ||
		(input.Plan == "free" && (input.CommunityLimit != 1 || input.ManagedBillingAllowed)) {
		http.Error(w, "invalid SaaS account change", 400)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var workspace string
	err = tx.QueryRow(r.Context(), `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var used, managedCommunities int
	err = tx.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER (WHERE billing_mode='managed_bot')
		FROM communities WHERE workspace_id=$1 AND status<>'archived'`, workspace).Scan(&used, &managedCommunities)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if used > input.CommunityLimit || (managedCommunities > 0 && !input.ManagedBillingAllowed) {
		http.Error(w, "existing communities exceed requested allowance", http.StatusConflict)
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE saas_accounts SET plan_code=$2,status=$3,
		community_limit=$4,managed_billing_allowed=$5,granted_by=$6,granted_reason=$7,updated_at=now()
		WHERE workspace_id=$1`, workspace, input.Plan, input.Status, input.CommunityLimit, input.ManagedBillingAllowed, operator, input.Reason)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
		VALUES($1,'saas_account_changed',jsonb_build_object('workspace_id',$2::uuid,'plan',$3::text,'status',$4::text,
		'community_limit',$5::integer,'managed_billing_allowed',$6::boolean,'reason',$7::text))`, operator, workspace, input.Plan, input.Status,
		input.CommunityLimit, input.ManagedBillingAllowed, input.Reason)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"workspace_id": workspace, "plan": input.Plan, "status": input.Status,
		"community_limit": input.CommunityLimit, "managed_billing_allowed": input.ManagedBillingAllowed})
}
