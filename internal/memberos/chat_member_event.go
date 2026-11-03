package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"memberos/internal/eventing"
	"memberos/internal/telegram"
	"time"

	"github.com/jackc/pgx/v5"
)

type memberUpdate struct {
	ID         int64 `json:"update_id"`
	ChatMember struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Date int64               `json:"date"`
		New  telegram.ChatMember `json:"new_chat_member"`
	} `json:"chat_member"`
}

func (a *app) processChatMember(ctx context.Context, tx pgx.Tx, updateID int64, raw []byte) error {
	var update memberUpdate
	if json.Unmarshal(raw, &update) != nil || update.ChatMember.New.User.ID <= 0 || update.ChatMember.Chat.ID == 0 {
		return eventing.InvalidEvent("invalid chat_member update")
	}
	var communityID, creatorID string
	err := tx.QueryRow(ctx, `SELECT id,creator_id FROM communities WHERE telegram_chat_id=$1`, update.ChatMember.Chat.ID).Scan(&communityID, &creatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.InvalidEvent("unknown community")
	}
	if err != nil {
		return err
	}
	observedAt := time.Unix(update.ChatMember.Date, 0).UTC()
	if update.ChatMember.Date <= 0 || observedAt.After(time.Now().Add(time.Minute)) {
		return eventing.InvalidEvent("invalid event timestamp")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO members(creator_id,telegram_user_id) VALUES($1,$2)
		ON CONFLICT (creator_id,telegram_user_id) DO NOTHING`, creatorID, update.ChatMember.New.User.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO access_observations(community_id,telegram_user_id,member_status,subscription_until,observed_at,source)
		VALUES($1,$2,$3,$4,$5,'chat_member') ON CONFLICT (community_id,telegram_user_id)
		DO UPDATE SET member_status=EXCLUDED.member_status,subscription_until=EXCLUDED.subscription_until,
		observed_at=EXCLUDED.observed_at,source=EXCLUDED.source
		WHERE access_observations.observed_at <= EXCLUDED.observed_at`, communityID, update.ChatMember.New.User.ID,
		update.ChatMember.New.Status, telegram.MemberUntil(update.ChatMember.New.UntilDate), observedAt); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,update_id,details)
		VALUES($1,$2,'chat_member_observed',$3,jsonb_build_object('status',$4))`, creatorID,
		update.ChatMember.New.User.ID, updateID, update.ChatMember.New.Status)
	return err
}
