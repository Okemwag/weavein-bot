package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"memberos/internal/notify"
	"memberos/internal/store"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s Service) RunJobs(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 10; i++ {
				more, err := s.ProcessOne(ctx)
				if err != nil {
					slog.Error("access job failed", "error", err)
					break
				}
				if !more {
					break
				}
			}
		}
	}
}

func (s Service) ProcessOne(ctx context.Context) (bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var jobID, communityID, action string
	var userID int64
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id,community_id,telegram_user_id,action,attempts FROM access_jobs
        WHERE status='pending' AND run_after<=now() ORDER BY run_after,id LIMIT 1 FOR UPDATE SKIP LOCKED`).
		Scan(&jobID, &communityID, &userID, &action, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var chatID, botID int64
	err = tx.QueryRow(ctx, `SELECT telegram_chat_id,billing_bot_id FROM communities
        WHERE id=$1 AND billing_mode='managed_bot'`, communityID).Scan(&chatID, &botID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, finishJob(ctx, tx, jobID, "failed", "community is not managed")
	}
	if err != nil {
		return false, err
	}
	if botID != s.BotID {
		return true, finishJob(ctx, tx, jobID, "failed", "bot identity mismatch")
	}
	if err := store.LockAccess(ctx, tx, communityID, userID); err != nil {
		return false, err
	}
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entitlements
        WHERE community_id=$1 AND telegram_user_id=$2 AND status='active' AND valid_until>now())
        OR EXISTS(SELECT 1 FROM access_overrides WHERE community_id=$1 AND telegram_user_id=$2
        AND active=true AND valid_until>now())`, communityID, userID).Scan(&eligible); err != nil {
		return false, err
	}
	if action == "approve" && !eligible {
		return true, finishJob(ctx, tx, jobID, "done", "no current entitlement")
	}
	if action == "remove" && eligible {
		return true, finishJob(ctx, tx, jobID, "done", "another entitlement grants access")
	}
	if action == "decline" && eligible {
		return true, finishJob(ctx, tx, jobID, "done", "membership became eligible")
	}
	member, err := s.Telegram.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return true, retryJob(ctx, tx, jobID, attempts, err.Error())
	}
	if member.Status == "creator" || member.Status == "administrator" {
		return true, finishJob(ctx, tx, jobID, "done", "creator or administrator protected")
	}
	switch action {
	case "approve":
		if member.Status == "member" {
			return true, finishJob(ctx, tx, jobID, "done", "")
		}
		if member.Status == "kicked" {
			var ok bool
			err = s.Telegram.Call(ctx, "unbanChatMember", url.Values{
				"chat_id": {strconv.FormatInt(chatID, 10)}, "user_id": {strconv.FormatInt(userID, 10)}, "only_if_banned": {"true"},
			}, &ok)
			if err != nil || !ok {
				return true, retryJob(ctx, tx, jobID, attempts, "Telegram unban failed")
			}
			return true, finishJob(ctx, tx, jobID, "done", "unbanned; member must request to join again")
		}
		var pending bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM join_requests WHERE community_id=$1
            AND telegram_user_id=$2 AND status='pending')`, communityID, userID).Scan(&pending); err != nil {
			return false, err
		}
		if !pending {
			return true, finishJob(ctx, tx, jobID, "done", "no pending join request")
		}
		var ok bool
		err = s.Telegram.Call(ctx, "approveChatJoinRequest", url.Values{
			"chat_id": {strconv.FormatInt(chatID, 10)}, "user_id": {strconv.FormatInt(userID, 10)},
		}, &ok)
		if err != nil || !ok {
			return true, retryJob(ctx, tx, jobID, attempts, "Telegram approval failed")
		}
		if _, err := tx.Exec(ctx, `UPDATE join_requests SET status='approved' WHERE community_id=$1 AND telegram_user_id=$2`, communityID, userID); err != nil {
			return false, err
		}
		return true, finishJob(ctx, tx, jobID, "awaiting_confirmation", "waiting for Telegram membership confirmation")
	case "remove":
		if member.Status == "left" || member.Status == "kicked" {
			return true, finishJob(ctx, tx, jobID, "done", "")
		}
		var ok bool
		err = s.Telegram.Call(ctx, "banChatMember", url.Values{
			"chat_id": {strconv.FormatInt(chatID, 10)}, "user_id": {strconv.FormatInt(userID, 10)},
		}, &ok)
		if err != nil || !ok {
			return true, retryJob(ctx, tx, jobID, attempts, "Telegram removal failed")
		}
		if err := notify.Queue(ctx, tx, s.BotID, userID, "access-removed:"+jobID,
			"Your community access has been removed after your paid entitlement ended. Open MemberOS to subscribe again or contact support."); err != nil {
			return false, err
		}
		return true, finishJob(ctx, tx, jobID, "done", "")
	case "decline":
		if member.Status == "member" || member.Status == "restricted" {
			return true, finishJob(ctx, tx, jobID, "done", "already a member")
		}
		var pending bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM join_requests WHERE community_id=$1
            AND telegram_user_id=$2 AND status='pending')`, communityID, userID).Scan(&pending); err != nil {
			return false, err
		}
		if !pending {
			return true, finishJob(ctx, tx, jobID, "done", "no pending join request")
		}
		var userChatID *int64
		var requestedAt time.Time
		var guidanceAttempted *time.Time
		if err := tx.QueryRow(ctx, `SELECT user_chat_id,requested_at,guidance_attempted_at FROM join_requests
			WHERE community_id=$1 AND telegram_user_id=$2 FOR UPDATE`, communityID, userID).
			Scan(&userChatID, &requestedAt, &guidanceAttempted); err != nil {
			return false, err
		}
		if guidanceAttempted == nil {
			message := "Your community join request needs an active membership. Open MemberOS to subscribe, then request access again. Contact support if you already paid."
			if strings.HasPrefix(s.AppOrigin, "https://") {
				message += " Rejoin: " + strings.TrimRight(s.AppOrigin, "/") + "/app/?community=" + url.QueryEscape(communityID)
			}
			sent := false
			if userChatID != nil && *userChatID > 0 && time.Since(requestedAt) < 5*time.Minute {
				var reply struct {
					MessageID int64 `json:"message_id"`
				}
				if err := s.Telegram.Call(ctx, "sendMessage", url.Values{
					"chat_id": {strconv.FormatInt(*userChatID, 10)}, "text": {message},
				}, &reply); err == nil && reply.MessageID > 0 {
					sent = true
				}
			}
			if !sent {
				if err := notify.Queue(ctx, tx, s.BotID, userID, fmt.Sprintf("join-ineligible:%s:%d", communityID, requestedAt.Unix()), message); err != nil {
					return false, err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE join_requests SET guidance_attempted_at=now()
				WHERE community_id=$1 AND telegram_user_id=$2`, communityID, userID); err != nil {
				return false, err
			}
		}
		var ok bool
		err = s.Telegram.Call(ctx, "declineChatJoinRequest", url.Values{
			"chat_id": {strconv.FormatInt(chatID, 10)}, "user_id": {strconv.FormatInt(userID, 10)},
		}, &ok)
		if err != nil || !ok {
			return true, retryJob(ctx, tx, jobID, attempts, "Telegram decline failed")
		}
		if _, err := tx.Exec(ctx, `UPDATE join_requests SET status='ineligible' WHERE community_id=$1
            AND telegram_user_id=$2 AND status='pending'`, communityID, userID); err != nil {
			return false, err
		}
		return true, finishJob(ctx, tx, jobID, "done", "")
	default:
		return true, finishJob(ctx, tx, jobID, "failed", "unknown access action")
	}
}

func finishJob(ctx context.Context, tx pgx.Tx, id, state, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE access_jobs SET status=$2,last_error=NULLIF($3,''),
	        finished_at=CASE WHEN $2='awaiting_confirmation' THEN NULL ELSE now() END,
        run_after=CASE WHEN $2='awaiting_confirmation' THEN now() ELSE run_after END WHERE id=$1`, id, state, reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func retryJob(ctx context.Context, tx pgx.Tx, id string, attempts int, reason string) error {
	next := attempts + 1
	status := "pending"
	if next >= 5 {
		status = "failed"
	}
	delay := time.Duration(1<<min(next, 6)) * time.Second
	_, err := tx.Exec(ctx, `UPDATE access_jobs SET status=$2,attempts=$3,last_error=$4,run_after=$5,
        finished_at=CASE WHEN $2='failed' THEN now() ELSE NULL END WHERE id=$1`,
		id, status, next, reason, time.Now().Add(delay))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
