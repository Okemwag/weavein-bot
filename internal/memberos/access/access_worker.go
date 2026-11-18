package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"memberos/internal/notify"
	"memberos/internal/store"
	"memberos/internal/telegram"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	maxJobAttempts = 8
	jobLease       = 2 * time.Minute
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

type job struct {
	id, community, action string
	user, chat            int64
	attempts              int
	subscription          *string
}

// outcome is what a job did at Telegram. It is applied in a short transaction
// after the calls return, so no database lock is held during network I/O.
type outcome struct {
	status, reason string
	retryErr       error

	removed, unbanned, approved, declined, renewalCanceled bool

	guidanceAttempted bool
	guidanceFallback  string
	requestedAt       time.Time
}

func done(reason string) outcome { return outcome{status: "done", reason: reason} }

func retry(err error, reason string) outcome { return outcome{retryErr: err, reason: reason} }

// ProcessOne claims one job, performs its Telegram calls without holding a
// transaction, then records the result. Every action checks Telegram state
// first, so a job interrupted between those steps is safe to run again.
func (s Service) ProcessOne(ctx context.Context) (bool, error) {
	if _, err := s.DB.Exec(ctx, `UPDATE access_jobs SET status='pending',claimed_at=NULL,run_after=now(),
		last_error='worker interrupted; rechecking Telegram state'
		WHERE status='running' AND claimed_at<now()-make_interval(secs => $1::int)`, int(jobLease.Seconds())); err != nil {
		return false, err
	}
	claimed, found, err := s.claim(ctx)
	if err != nil || !found {
		return false, err
	}
	if claimed == nil {
		return true, nil
	}
	result := s.execute(ctx, *claimed)
	// Record what Telegram already did even if shutdown has begun.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return true, s.record(recordCtx, *claimed, result)
}

// claim returns found=false when no job is due. A nil job with found=true means
// the job was settled during the claim and needs no Telegram call.
func (s Service) claim(ctx context.Context) (*job, bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var j job
	err = tx.QueryRow(ctx, `SELECT j.id,j.community_id,j.telegram_user_id,j.action,j.attempts,j.subscription_id FROM access_jobs j
		WHERE j.status='pending' AND j.run_after<=now() AND NOT EXISTS(SELECT 1 FROM access_jobs r
			WHERE r.status='running' AND r.community_id=j.community_id AND r.telegram_user_id=j.telegram_user_id)
		ORDER BY j.run_after,j.id LIMIT 1 FOR UPDATE OF j SKIP LOCKED`).
		Scan(&j.id, &j.community, &j.user, &j.action, &j.attempts, &j.subscription)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	settle := func(state, reason string) (*job, bool, error) {
		// finishJob commits the claim transaction.
		return nil, true, finishJob(ctx, tx, j.id, state, reason)
	}
	var botID int64
	err = tx.QueryRow(ctx, `SELECT telegram_chat_id,billing_bot_id FROM communities
		WHERE id=$1 AND billing_mode='managed_bot'`, j.community).Scan(&j.chat, &botID)
	if errors.Is(err, pgx.ErrNoRows) {
		return settle("failed", "community is not managed")
	}
	if err != nil {
		return nil, false, err
	}
	if botID != s.BotID {
		return settle("failed", "bot identity mismatch")
	}
	if err := store.LockAccess(ctx, tx, j.community, j.user); err != nil {
		return nil, false, err
	}
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT member_has_access($1,$2)`, j.community, j.user).Scan(&eligible); err != nil {
		return nil, false, err
	}
	switch {
	case j.action == "approve" && !eligible:
		return settle("done", "no current entitlement")
	case j.action == "remove" && eligible:
		return settle("done", "another entitlement grants access")
	case j.action == "decline" && eligible:
		return settle("done", "membership became eligible")
	}
	if _, err := tx.Exec(ctx, `UPDATE access_jobs SET status='running',claimed_at=now() WHERE id=$1`, j.id); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return &j, true, nil
}

func (s Service) execute(ctx context.Context, j job) outcome {
	if j.action == "cancel_renewal" {
		return s.executeCancelRenewal(ctx, j)
	}
	member, err := s.Telegram.GetChatMember(ctx, j.chat, j.user)
	if err != nil {
		return retry(err, err.Error())
	}
	if member.Status == "creator" || member.Status == "administrator" {
		return done("creator or administrator protected")
	}
	params := url.Values{"chat_id": {strconv.FormatInt(j.chat, 10)}, "user_id": {strconv.FormatInt(j.user, 10)}}
	switch j.action {
	case "approve":
		return s.executeApprove(ctx, j, member.Status, params)
	case "remove":
		if member.Status == "left" || member.Status == "kicked" {
			return done("")
		}
		var ok bool
		if err := s.Telegram.Call(ctx, "banChatMember", params, &ok); err != nil || !ok {
			return retry(err, "Telegram removal failed")
		}
		return outcome{status: "done", removed: true}
	case "decline":
		return s.executeDecline(ctx, j, member.Status, params)
	}
	return outcome{status: "failed", reason: "unknown access action"}
}

func (s Service) executeApprove(ctx context.Context, j job, status string, params url.Values) outcome {
	if status == "member" {
		return done("")
	}
	if status == "kicked" {
		var ok bool
		unban := url.Values{"chat_id": params["chat_id"], "user_id": params["user_id"], "only_if_banned": {"true"}}
		if err := s.Telegram.Call(ctx, "unbanChatMember", unban, &ok); err != nil || !ok {
			return retry(err, "Telegram unban failed")
		}
		return outcome{status: "done", reason: "unbanned; member must request to join again", unbanned: true}
	}
	var pending bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM join_requests WHERE community_id=$1
		AND telegram_user_id=$2 AND status='pending')`, j.community, j.user).Scan(&pending); err != nil {
		return retry(err, "join request lookup failed")
	}
	if !pending {
		return done("no pending join request")
	}
	var ok bool
	if err := s.Telegram.Call(ctx, "approveChatJoinRequest", params, &ok); err != nil || !ok {
		return retry(err, "Telegram approval failed")
	}
	return outcome{status: "awaiting_confirmation", reason: "waiting for Telegram membership confirmation", approved: true}
}

func (s Service) executeDecline(ctx context.Context, j job, status string, params url.Values) outcome {
	if status == "member" || status == "restricted" {
		return done("already a member")
	}
	var requestStatus string
	var userChatID *int64
	var requestedAt time.Time
	var guidanceAttempted *time.Time
	err := s.DB.QueryRow(ctx, `SELECT status,user_chat_id,requested_at,guidance_attempted_at FROM join_requests
		WHERE community_id=$1 AND telegram_user_id=$2`, j.community, j.user).
		Scan(&requestStatus, &userChatID, &requestedAt, &guidanceAttempted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && requestStatus != "pending") {
		return done("no pending join request")
	}
	if err != nil {
		return retry(err, "join request lookup failed")
	}
	result := outcome{requestedAt: requestedAt}
	if guidanceAttempted == nil {
		message := "You need a membership to join this community. Subscribe in MemberOS, then send your join request again. Already paid? Tap Need help? in MemberOS."
		if strings.HasPrefix(s.AppOrigin, "https://") {
			message += "\n\nSubscribe: " + strings.TrimRight(s.AppOrigin, "/") + "/app/?community=" + url.QueryEscape(j.community)
		}
		result.guidanceAttempted = true
		sent := false
		if userChatID != nil && *userChatID > 0 && time.Since(requestedAt) < 5*time.Minute {
			var reply struct {
				MessageID int64 `json:"message_id"`
			}
			sent = s.Telegram.Call(ctx, "sendMessage", url.Values{
				"chat_id": {strconv.FormatInt(*userChatID, 10)}, "text": {message},
			}, &reply) == nil && reply.MessageID > 0
		}
		if !sent {
			result.guidanceFallback = message
		}
	}
	var ok bool
	if err := s.Telegram.Call(ctx, "declineChatJoinRequest", params, &ok); err != nil || !ok {
		result.retryErr, result.reason = err, "Telegram decline failed"
		return result
	}
	result.status, result.declined = "done", true
	return result
}

func (s Service) executeCancelRenewal(ctx context.Context, j job) outcome {
	if j.subscription == nil {
		return outcome{status: "failed", reason: "renewal cancellation has no subscription"}
	}
	var renew, charge string
	err := s.DB.QueryRow(ctx, `SELECT s.auto_renew_state,coalesce((SELECT p.telegram_charge_id FROM payments p
		WHERE p.subscription_id=s.id AND p.is_first_recurring ORDER BY p.paid_at LIMIT 1),'')
		FROM subscriptions s WHERE s.id=$1`, *j.subscription).Scan(&renew, &charge)
	if err != nil {
		return retry(err, "subscription lookup failed")
	}
	if renew != "on" {
		return done("renewal already off")
	}
	if charge == "" {
		return outcome{status: "failed", reason: "subscription has no first charge to cancel"}
	}
	var ok bool
	if err := s.Telegram.Call(ctx, "editUserStarSubscription", url.Values{
		"user_id": {strconv.FormatInt(j.user, 10)}, "telegram_payment_charge_id": {charge}, "is_canceled": {"true"},
	}, &ok); err != nil || !ok {
		var apiErr telegram.StatusError
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusBadRequest {
			return outcome{status: "failed", reason: "Telegram refused renewal cancellation; check the subscription"}
		}
		return retry(err, "Telegram renewal cancellation failed")
	}
	return outcome{status: "done", renewalCanceled: true}
}

func (s Service) record(ctx context.Context, j job, result outcome) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := store.LockAccess(ctx, tx, j.community, j.user); err != nil {
		return err
	}
	if result.guidanceAttempted {
		if result.guidanceFallback != "" {
			key := fmt.Sprintf("join-ineligible:%s:%d", j.community, result.requestedAt.Unix())
			if err := notify.Queue(ctx, tx, s.BotID, j.user, key, result.guidanceFallback); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE join_requests SET guidance_attempted_at=now()
			WHERE community_id=$1 AND telegram_user_id=$2`, j.community, j.user); err != nil {
			return err
		}
	}
	if result.status == "" {
		return retryJob(ctx, tx, j, result)
	}
	if result.approved {
		if _, err := tx.Exec(ctx, `UPDATE join_requests SET status='approved' WHERE community_id=$1 AND telegram_user_id=$2`, j.community, j.user); err != nil {
			return err
		}
	}
	if result.declined {
		if _, err := tx.Exec(ctx, `UPDATE join_requests SET status='ineligible' WHERE community_id=$1
			AND telegram_user_id=$2 AND status='pending'`, j.community, j.user); err != nil {
			return err
		}
	}
	if result.unbanned {
		if err := s.queueRejoinNotice(ctx, tx, j); err != nil {
			return err
		}
	}
	if result.removed {
		if err := s.settleRemoval(ctx, tx, j); err != nil {
			return err
		}
	}
	if result.renewalCanceled {
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET auto_renew_state='off',canceled_at=coalesce(canceled_at,now()),updated_at=now(),
			state=CASE WHEN state IN ('active','payment_failed') THEN 'cancel_scheduled' ELSE state END WHERE id=$1`, *j.subscription); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO subscription_events(subscription_id,event_key,kind)
			VALUES($1,$2,'renewal_canceled_by_system') ON CONFLICT (event_key) DO NOTHING`, *j.subscription, "system-cancel:"+j.id); err != nil {
			return err
		}
	}
	return finishJob(ctx, tx, j.id, result.status, result.reason)
}

// settleRemoval runs after the ban. A payment can land while Telegram is being
// called, so access is checked again and a wrongly removed member is restored.
func (s Service) settleRemoval(ctx context.Context, tx pgx.Tx, j job) error {
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT member_has_access($1,$2)`, j.community, j.user).Scan(&eligible); err != nil {
		return err
	}
	if eligible {
		_, err := tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
			VALUES($1,$2,'approve',$3) ON CONFLICT (dedupe_key) DO NOTHING`, j.community, j.user, "approve:restore:"+j.id)
		return err
	}
	return notify.Queue(ctx, tx, s.BotID, j.user, "access-removed:"+j.id,
		"Your membership ended, so you've been removed from the community. You can join again anytime in MemberOS.")
}

// queueRejoinNotice tells a restored member how to return. Telegram does not
// let a bot add someone back, so the member has to request access once more.
func (s Service) queueRejoinNotice(ctx context.Context, tx pgx.Tx, j job) error {
	var title, link string
	if err := tx.QueryRow(ctx, `SELECT title,coalesce(managed_join_link,'') FROM communities WHERE id=$1`, j.community).Scan(&title, &link); err != nil {
		return err
	}
	return notify.Queue(ctx, tx, s.BotID, j.user, "access-restored:"+j.id,
		fmt.Sprintf("Your access to %s is back. Tap this link and send the join request, and you'll be approved automatically: %s", title, link))
}

func finishJob(ctx context.Context, tx pgx.Tx, id, state, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE access_jobs SET status=$2,last_error=NULLIF($3,''),claimed_at=NULL,
		finished_at=CASE WHEN $2='awaiting_confirmation' THEN NULL ELSE now() END,
		run_after=CASE WHEN $2='awaiting_confirmation' THEN now() ELSE run_after END WHERE id=$1`, id, state, reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// retryJob backs off for about an hour in total, so a Telegram outage does not
// strand jobs. Rate limits wait as long as Telegram asks and use no attempt.
func retryJob(ctx context.Context, tx pgx.Tx, j job, result outcome) error {
	next := j.attempts + 1
	delay := time.Duration(15<<min(j.attempts, 8)) * time.Second
	var apiErr telegram.StatusError
	if errors.As(result.retryErr, &apiErr) && apiErr.Code == http.StatusTooManyRequests {
		next = j.attempts
		delay = apiErr.RetryDelay()
	}
	status := "pending"
	if next >= maxJobAttempts {
		status = "failed"
	}
	_, err := tx.Exec(ctx, `UPDATE access_jobs SET status=$2,attempts=$3,last_error=$4,run_after=$5,claimed_at=NULL,
		finished_at=CASE WHEN $2='failed' THEN now() ELSE NULL END WHERE id=$1`,
		j.id, status, next, result.reason, time.Now().Add(delay))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
