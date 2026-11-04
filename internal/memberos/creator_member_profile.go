package memberos

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func memberUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid member", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func (a *app) creatorMemberProfile(w http.ResponseWriter, r *http.Request) {
	access, ok := a.requireCreatorCommunity(w, r)
	if !ok {
		return
	}
	user, ok := memberUserID(w, r)
	if !ok {
		return
	}
	var name, username string
	err := a.db.QueryRow(r.Context(), `SELECT m.first_name,m.username FROM members m JOIN communities c ON c.creator_id=m.creator_id
 WHERE c.id=$1 AND m.telegram_user_id=$2`, access.ID, user).Scan(&name, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	result := map[string]any{"telegram_user_id": user, "first_name": name, "username": username, "community_id": access.ID, "mode": access.Mode}
	var observedStatus *string
	var observedAt *time.Time
	err = a.db.QueryRow(r.Context(), `SELECT member_status,observed_at FROM access_observations
 WHERE community_id=$1 AND telegram_user_id=$2`, access.ID, user).Scan(&observedStatus, &observedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "database error", 500)
		return
	}
	result["access_status"] = observedStatus
	result["observed_at"] = observedAt
	if access.Mode == "managed_bot" {
		rows, err := a.db.Query(r.Context(), `SELECT s.id,s.state,s.auto_renew_state,s.first_paid_at,s.paid_through,
  p.name,pv.version,pv.price_stars FROM subscriptions s
  LEFT JOIN plan_versions pv ON pv.id=s.plan_version_id LEFT JOIN plans p ON p.id=pv.plan_id
  WHERE s.community_id=$1 AND s.telegram_user_id=$2 ORDER BY s.first_paid_at DESC LIMIT 100`, access.ID, user)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		subscriptions := make([]map[string]any, 0)
		for rows.Next() {
			var id, state, renew string
			var first, through time.Time
			var plan *string
			var version, price *int
			if rows.Scan(&id, &state, &renew, &first, &through, &plan, &version, &price) != nil {
				rows.Close()
				http.Error(w, "database error", 500)
				return
			}
			subscriptions = append(subscriptions, map[string]any{"id": id, "state": state, "auto_renew_state": renew, "subscribed_at": first, "paid_through": through, "plan": plan, "plan_version": version, "price_stars": price})
		}
		if rows.Err() != nil {
			rows.Close()
			http.Error(w, "database error", 500)
			return
		}
		rows.Close()
		result["subscriptions"] = subscriptions
		payments, err := a.db.Query(r.Context(), `SELECT p.id,p.price_stars,p.paid_at,p.provider_expiration,p.refunded_at,p.is_first_recurring
  FROM payments p JOIN subscriptions s ON s.id=p.subscription_id WHERE s.community_id=$1 AND s.telegram_user_id=$2
  ORDER BY p.paid_at DESC LIMIT 100`, access.ID, user)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		history := make([]map[string]any, 0)
		for payments.Next() {
			var id string
			var amount int
			var paid, through time.Time
			var refunded *time.Time
			var first bool
			if payments.Scan(&id, &amount, &paid, &through, &refunded, &first) != nil {
				payments.Close()
				http.Error(w, "database error", 500)
				return
			}
			history = append(history, map[string]any{"id": id, "amount_stars": amount, "paid_at": paid, "paid_through": through, "refunded_at": refunded, "first": first})
		}
		if payments.Err() != nil {
			payments.Close()
			http.Error(w, "database error", 500)
			return
		}
		payments.Close()
		result["payments"] = history
		events, err := a.db.Query(r.Context(), `SELECT e.kind,e.occurred_at,e.details FROM subscription_events e
  JOIN subscriptions s ON s.id=e.subscription_id WHERE s.community_id=$1 AND s.telegram_user_id=$2
  ORDER BY e.occurred_at DESC LIMIT 100`, access.ID, user)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		timeline := make([]map[string]any, 0)
		for events.Next() {
			var kind string
			var at time.Time
			var details json.RawMessage
			if events.Scan(&kind, &at, &details) != nil {
				events.Close()
				http.Error(w, "database error", 500)
				return
			}
			timeline = append(timeline, map[string]any{"kind": kind, "at": at, "details": details})
		}
		if events.Err() != nil {
			events.Close()
			http.Error(w, "database error", 500)
			return
		}
		events.Close()
		result["events"] = timeline
	}
	notes, err := a.db.Query(r.Context(), `SELECT id,author_telegram_user_id,body,created_at FROM member_notes
 WHERE community_id=$1 AND telegram_user_id=$2 ORDER BY created_at DESC LIMIT 100`, access.ID, user)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	list := make([]map[string]any, 0)
	for notes.Next() {
		var id, body string
		var author int64
		var at time.Time
		if notes.Scan(&id, &author, &body, &at) != nil {
			notes.Close()
			http.Error(w, "database error", 500)
			return
		}
		list = append(list, map[string]any{"id": id, "author_telegram_user_id": author, "body": body, "created_at": at})
	}
	if notes.Err() != nil {
		notes.Close()
		http.Error(w, "database error", 500)
		return
	}
	notes.Close()
	result["private_notes"] = list
	writeJSON(w, 200, result)
}

func (a *app) creatorMemberNote(w http.ResponseWriter, r *http.Request) {
	access, ok := a.requireCreatorCommunity(w, r)
	if !ok {
		return
	}
	user, ok := memberUserID(w, r)
	if !ok {
		return
	}
	var input struct {
		Body string `json:"body"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || len(strings.TrimSpace(input.Body)) < 1 || len(input.Body) > 2000 {
		http.Error(w, "invalid note", http.StatusBadRequest)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO member_notes(community_id,telegram_user_id,author_telegram_user_id,body)
 SELECT c.id,$2,$3,$4 FROM communities c JOIN members m ON m.creator_id=c.creator_id
 WHERE c.id=$1 AND m.telegram_user_id=$2 RETURNING id`, access.ID, user, access.User, strings.TrimSpace(input.Body)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(subject_telegram_user_id,action,details)
 VALUES($1,'member_note_added',jsonb_build_object('community_id',$2,'member_id',$3,'note_id',$4))`, access.User, access.ID, user, id)
	if err != nil || tx.Commit(r.Context()) != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}
