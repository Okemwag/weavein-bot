package memberos

import (
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
	var creatorID, mode string
	var chatID int64
	err = a.db.QueryRow(r.Context(), `SELECT creator_id,telegram_chat_id,billing_mode FROM communities WHERE id::text=$1 AND status='published'`, communityID).Scan(&creatorID, &chatID, &mode)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	_, err = a.db.Exec(r.Context(), `INSERT INTO members(creator_id,telegram_user_id,first_name,username)
        VALUES($1,$2,coalesce((SELECT first_name FROM telegram_profiles WHERE telegram_user_id=$2),''),
        coalesce((SELECT username FROM telegram_profiles WHERE telegram_user_id=$2),''))
        ON CONFLICT (creator_id, telegram_user_id) DO UPDATE SET first_name=EXCLUDED.first_name,
        username=EXCLUDED.username,verified_at=now()`, creatorID, userID)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	member, lookupErr := a.telegram.GetChatMember(r.Context(), chatID, userID)
	if lookupErr == nil {
		_, err = a.db.Exec(r.Context(), `INSERT INTO access_observations(community_id,telegram_user_id,member_status,subscription_until,observed_at,source)
            VALUES($1,$2,$3,$4,now(),'getChatMember') ON CONFLICT (community_id,telegram_user_id)
            DO UPDATE SET member_status=EXCLUDED.member_status,subscription_until=EXCLUDED.subscription_until,
			observed_at=EXCLUDED.observed_at,source=EXCLUDED.source`, communityID, userID, member.Status, telegram.MemberUntil(member.UntilDate))
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
	}
	response := map[string]any{"community_id": communityID, "telegram_user_id": userID, "access_status": "unknown", "fresh": lookupErr == nil}
	if mode == "managed_bot" {
		var subscriptionID, subState, renewState string
		var paidThrough time.Time
		err = a.db.QueryRow(r.Context(), `SELECT id,state,auto_renew_state,paid_through FROM subscriptions
            WHERE community_id::text=$1 AND telegram_user_id=$2 ORDER BY last_payment_at DESC LIMIT 1`, communityID, userID).
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
		if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM entitlements WHERE community_id::text=$1
            AND telegram_user_id=$2 AND status='active' AND valid_until>now()),
            (SELECT managed_join_link FROM communities WHERE id::text=$1)`, communityID, userID).Scan(&eligible, &joinLink); err != nil {
			http.Error(w, "database error", 500)
			return
		}
		response["entitled"] = eligible
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
        WHERE community_id::text=$1 AND telegram_user_id=$2`, communityID, userID).Scan(&status, &until, &observed, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		if lookupErr != nil && mode == "native_channel" {
			http.Error(w, "Telegram membership unavailable", http.StatusBadGateway)
			return
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
