package memberos

import (
	"context"
	"errors"
	"memberos/internal/telegram"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) membership(w http.ResponseWriter, r *http.Request) {
	userID, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	communityID := r.URL.Query().Get("community_id")
	if !validUUID(communityID) {
		http.Error(w, "invalid community", http.StatusBadRequest)
		return
	}
	var creatorID, mode string
	var chatID int64
	err = a.db.QueryRow(r.Context(), `SELECT creator_id,telegram_chat_id,billing_mode FROM communities WHERE id=$1 AND status='published'`, communityID).Scan(&creatorID, &chatID, &mode)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	// Reuse a recent observation: this endpoint runs whenever the app regains
	// focus, and each Telegram lookup spends the bot's shared rate limit.
	var recentlyObserved bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM access_observations WHERE community_id=$1
		AND telegram_user_id=$2 AND observed_at>now()-interval '1 minute')`, communityID, userID).Scan(&recentlyObserved); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	var member telegram.ChatMember
	var lookupErr error
	if !recentlyObserved {
		member, lookupErr = a.telegram.GetChatMember(r.Context(), chatID, userID)
		if lookupErr == nil {
			if err := a.recordMembershipLookup(r.Context(), creatorID, communityID, userID, member); err != nil {
				http.Error(w, "database error", 500)
				return
			}
		}
	}
	response := map[string]any{"community_id": communityID, "telegram_user_id": userID, "access_status": "unknown", "fresh": lookupErr == nil}
	if mode == "managed_bot" {
		var subscriptionID, subState, renewState string
		var paidThrough time.Time
		err = a.db.QueryRow(r.Context(), `SELECT id,state,auto_renew_state,paid_through FROM subscriptions
            WHERE community_id=$1 AND telegram_user_id=$2 ORDER BY last_payment_at DESC LIMIT 1`, communityID, userID).
			Scan(&subscriptionID, &subState, &renewState, &paidThrough)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "database error", 500)
			return
		}
		response["billing_status"] = "no_subscription"
		if err == nil {
			response["subscription_id"] = subscriptionID
			response["billing_status"] = subState
			response["auto_renew_state"] = renewState
			response["paid_through"] = paidThrough
		}
		var eligible bool
		var joinLink *string
		if err := a.db.QueryRow(r.Context(), `SELECT member_has_access($1,$2),
            (SELECT managed_join_link FROM communities WHERE id=$1)`, communityID, userID).Scan(&eligible, &joinLink); err != nil {
			http.Error(w, "database error", 500)
			return
		}
		response["entitled"] = eligible
		renewing, err := a.renewingSubscriptions(r.Context(), communityID, userID)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
		response["renewing_subscriptions"] = renewing
		if eligible {
			response["join_link"] = joinLink
		}
	} else {
		response["billing_status"] = "unavailable"
	}
	var status, source string
	var until *time.Time
	var observed time.Time
	err = a.db.QueryRow(r.Context(), `SELECT member_status,subscription_until,observed_at,source FROM access_observations
        WHERE community_id=$1 AND telegram_user_id=$2`, communityID, userID).Scan(&status, &until, &observed, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		if lookupErr != nil && mode == "native_channel" {
			http.Error(w, "Telegram membership unavailable", http.StatusBadGateway)
			return
		}
		if lookupErr == nil {
			response["access_status"] = member.Status
		}
		writeJSON(w, 200, response)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	response["access_status"] = status
	response["subscription_until"] = until
	response["observed_at"] = observed
	response["observation_source"] = source
	writeJSON(w, 200, response)
}

// recordMembershipLookup saves what Telegram reported. Someone who is merely
// viewing a community is not recorded, so browsing cannot add rows to a creator's
// member list or to the reconciliation queue.
func (a *app) recordMembershipLookup(ctx context.Context, creatorID, communityID string, userID int64, member telegram.ChatMember) error {
	present := member.Status == "member" || member.Status == "restricted" || member.Status == "administrator" || member.Status == "creator"
	if present {
		if _, err := a.db.Exec(ctx, `INSERT INTO members(creator_id,telegram_user_id,first_name,username)
			VALUES($1,$2,coalesce((SELECT first_name FROM telegram_profiles WHERE telegram_user_id=$2),''),
			coalesce((SELECT username FROM telegram_profiles WHERE telegram_user_id=$2),''))
			ON CONFLICT (creator_id,telegram_user_id) DO UPDATE SET first_name=EXCLUDED.first_name,
			username=EXCLUDED.username,verified_at=now()`, creatorID, userID); err != nil {
			return err
		}
		_, err := a.db.Exec(ctx, `INSERT INTO access_observations(community_id,telegram_user_id,member_status,subscription_until,observed_at,source)
			VALUES($1,$2,$3,$4,now(),'getChatMember') ON CONFLICT (community_id,telegram_user_id)
			DO UPDATE SET member_status=EXCLUDED.member_status,subscription_until=EXCLUDED.subscription_until,
			observed_at=EXCLUDED.observed_at,source=EXCLUDED.source`, communityID, userID, member.Status, telegram.MemberUntil(member.UntilDate))
		return err
	}
	_, err := a.db.Exec(ctx, `UPDATE access_observations SET member_status=$3,subscription_until=$4,observed_at=now(),source='getChatMember'
		WHERE community_id=$1 AND telegram_user_id=$2`, communityID, userID, member.Status, telegram.MemberUntil(member.UntilDate))
	return err
}

// renewingSubscriptions lists every subscription Telegram may still charge, so a
// member holding more than one can see and cancel each of them.
func (a *app) renewingSubscriptions(ctx context.Context, communityID string, userID int64) ([]map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT id,paid_through FROM subscriptions WHERE community_id=$1 AND telegram_user_id=$2
		AND auto_renew_state='on' AND state NOT IN ('expired','refunded') ORDER BY first_paid_at`, communityID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id string
		var through time.Time
		if err := rows.Scan(&id, &through); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "paid_through": through})
	}
	return items, rows.Err()
}
