package memberos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"memberos/internal/eventing"
	"memberos/internal/memberos/billing"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) processJoinRequest(ctx context.Context, tx pgx.Tx, updateID int64, raw []byte) error {
	var wrapper struct {
		Request struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Date       int64 `json:"date"`
			UserChatID int64 `json:"user_chat_id"`
		} `json:"chat_join_request"`
	}
	if json.Unmarshal(raw, &wrapper) != nil {
		return eventing.InvalidEvent("invalid join request JSON")
	}
	req := wrapper.Request
	if req.Chat.ID == 0 || req.From.ID <= 0 || req.Date <= 0 {
		return eventing.InvalidEvent("invalid join request fields")
	}
	var communityID, creatorID string
	err := tx.QueryRow(ctx, `SELECT id,creator_id FROM communities WHERE telegram_chat_id=$1 AND billing_mode='managed_bot' AND status='published'`, req.Chat.ID).Scan(&communityID, &creatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.IgnoredEvent("join request for unmanaged chat")
	}
	if err != nil {
		return err
	}
	requestedAt := time.Unix(req.Date, 0).UTC()
	if requestedAt.After(time.Now().Add(time.Minute)) {
		return eventing.InvalidEvent("join request from future")
	}
	_, err = tx.Exec(ctx, `INSERT INTO join_requests(community_id,telegram_user_id,requested_at,status,user_chat_id)
		VALUES($1,$2,$3,'pending',$4) ON CONFLICT (community_id,telegram_user_id)
		DO UPDATE SET requested_at=EXCLUDED.requested_at,status='pending',user_chat_id=EXCLUDED.user_chat_id,
		guidance_attempted_at=NULL`, communityID, req.From.ID, requestedAt, req.UserChatID)
	if err != nil {
		return err
	}
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT member_has_access($1,$2)`, communityID, req.From.ID).Scan(&eligible)
	if err != nil {
		return err
	}
	action := "decline"
	if eligible {
		action = "approve"
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_jobs(community_id,telegram_user_id,action,dedupe_key)
        VALUES($1,$2,$3,$4) ON CONFLICT (dedupe_key) DO NOTHING`, communityID, req.From.ID, action, fmt.Sprintf("%s:join:%d", action, updateID))
	if err != nil {
		return err
	}
	return billing.AuditEvent(ctx, tx, creatorID, req.From.ID, "join_requested", updateID)
}
