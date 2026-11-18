package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"memberos/internal/eventing"
	"memberos/internal/notify"
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
		return eventing.IgnoredEvent("chat is not a connected community")
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
	if update.ChatMember.New.Status == "member" {
		if _, err := tx.Exec(ctx, `UPDATE access_jobs SET status='done',last_error=NULL,finished_at=now()
            WHERE community_id=$1 AND telegram_user_id=$2 AND action='approve' AND status='awaiting_confirmation'`,
			communityID, update.ChatMember.New.User.ID); err != nil {
			return err
		}
		var subID, title, joinLink, support, rules, guide string
		var botID int64
		err = tx.QueryRow(ctx, `SELECT e.subscription_id,c.title,coalesce(c.managed_join_link,''),cr.support_contact,c.billing_bot_id,c.rules,c.content_guide
            FROM entitlements e JOIN communities c ON c.id=e.community_id JOIN creators cr ON cr.id=c.creator_id
            WHERE e.community_id=$1 AND e.telegram_user_id=$2 AND e.status='active' AND e.valid_until>now()
            AND c.billing_mode='managed_bot' ORDER BY e.valid_until DESC LIMIT 1`, communityID, update.ChatMember.New.User.ID).
			Scan(&subID, &title, &joinLink, &support, &botID, &rules, &guide)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			message := fmt.Sprintf("Welcome to %s! You're in. Open the community: %s", title, joinLink)
			if rules != "" {
				message += "\n\nRules: " + rules
			}
			if guide != "" {
				message += "\n\nWhere to begin: " + guide
			}
			message += "\n\nNeed help? " + support
			if err := notify.Queue(ctx, tx, botID, update.ChatMember.New.User.ID, "welcome:"+subID, message); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,update_id,details)
		VALUES($1,$2,'chat_member_observed',$3,jsonb_build_object('status',$4::text))`, creatorID,
		update.ChatMember.New.User.ID, updateID, update.ChatMember.New.Status)
	return err
}
