package access

import (
	"context"
	"fmt"
	"log/slog"
	"memberos/internal/telegram"
	"time"
)

func (s Service) RunReconciliation(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		if err := s.ReconcileKnown(ctx); err != nil {
			slog.Error("access reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReconcileKnown checks only identities already known to MemberOS. Telegram does
// not expose an unrestricted member list through this bot API.
func (s Service) ReconcileKnown(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT id,telegram_chat_id FROM communities
        WHERE billing_mode='managed_bot' AND status='published' AND billing_bot_id=$1`, s.BotID)
	if err != nil {
		return err
	}
	type community struct {
		id   string
		chat int64
	}
	var communities []community
	for rows.Next() {
		var c community
		if err := rows.Scan(&c.id, &c.chat); err != nil {
			rows.Close()
			return err
		}
		communities = append(communities, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, c := range communities {
		bot, err := s.Telegram.GetChatMember(ctx, c.chat, s.BotID)
		if err != nil || bot.Status != "administrator" || !bot.CanInviteUsers || !bot.CanRestrictMembers {
			reason := "bot lacks required permissions"
			if err != nil {
				reason = err.Error()
			}
			state := "degraded"
			if bot.Status == "left" || bot.Status == "kicked" {
				state = "disconnected"
			}
			if err := s.setCommunityHealth(ctx, c.id, state, reason); err != nil {
				return err
			}
			continue
		}
		if err := s.setCommunityHealth(ctx, c.id, "healthy", ""); err != nil {
			return err
		}
		members, err := s.DB.Query(ctx, `WITH known AS (
            SELECT telegram_user_id FROM entitlements WHERE community_id=$1
            UNION SELECT telegram_user_id FROM access_observations WHERE community_id=$1
			UNION SELECT telegram_user_id FROM access_overrides WHERE community_id=$1
            ) SELECT k.telegram_user_id FROM known k
            LEFT JOIN access_observations o ON o.community_id=$1 AND o.telegram_user_id=k.telegram_user_id
            ORDER BY o.observed_at NULLS FIRST,k.telegram_user_id LIMIT 25`, c.id)
		if err != nil {
			return err
		}
		var users []int64
		for members.Next() {
			var id int64
			if err := members.Scan(&id); err != nil {
				members.Close()
				return err
			}
			users = append(users, id)
		}
		if err := members.Err(); err != nil {
			members.Close()
			return err
		}
		members.Close()
		for _, user := range users {
			member, err := s.Telegram.GetChatMember(ctx, c.chat, user)
			if err != nil {
				slog.Warn("member reconciliation lookup failed", "community_id", c.id, "user_id", user, "error", err)
				continue
			}
			_, err = s.DB.Exec(ctx, `INSERT INTO access_observations(community_id,telegram_user_id,member_status,subscription_until,observed_at,source)
				VALUES($1,$2,$3,$4,now(),'getChatMember') ON CONFLICT (community_id,telegram_user_id)
                DO UPDATE SET member_status=EXCLUDED.member_status,subscription_until=EXCLUDED.subscription_until,
				observed_at=EXCLUDED.observed_at,source=EXCLUDED.source`, c.id, user, member.Status, telegram.MemberUntil(member.UntilDate))
			if err != nil {
				return err
			}
			if member.Status == "creator" || member.Status == "administrator" {
				continue
			}
			var entitled, pending bool
			err = s.DB.QueryRow(ctx, `SELECT (EXISTS(SELECT 1 FROM entitlements WHERE community_id=$1 AND telegram_user_id=$2
                AND status='active' AND valid_until>now()) OR EXISTS(SELECT 1 FROM access_overrides
                WHERE community_id=$1 AND telegram_user_id=$2 AND active=true AND valid_until>now())),
                EXISTS(SELECT 1 FROM join_requests WHERE community_id=$1
                AND telegram_user_id=$2 AND status='pending')`, c.id, user).Scan(&entitled, &pending)
			if err != nil {
				return err
			}
			present := member.Status == "member" || member.Status == "restricted"
			if present {
				if _, err := s.DB.Exec(ctx, `UPDATE access_jobs SET status='done',last_error=NULL,finished_at=now()
                    WHERE community_id=$1 AND telegram_user_id=$2 AND action='approve' AND status='awaiting_confirmation'`, c.id, user); err != nil {
					return err
				}
			} else {
				if _, err := s.DB.Exec(ctx, `UPDATE access_jobs SET status='failed',last_error='approval not confirmed by Telegram',finished_at=now()
                    WHERE community_id=$1 AND telegram_user_id=$2 AND action='approve' AND status='awaiting_confirmation'
                    AND run_after<now()-interval '15 minutes'`, c.id, user); err != nil {
					return err
				}
			}
			action := ""
			if !entitled && present {
				action = "remove"
			}
			if entitled && pending && !present {
				action = "approve"
			}
			if action != "" {
				key := fmt.Sprintf("reconcile:%s:%d:%s:%s", c.id, user, action, time.Now().UTC().Format("2006-01-02T15"))
				if _, err := s.DB.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
                    VALUES($1,$2,$3,$4) ON CONFLICT (dedupe_key) DO NOTHING`, c.id, user, action, key); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
