package access

import (
	"context"
	"fmt"
	"memberos/internal/notify"
	"time"
)

func (s Service) setCommunityHealth(ctx context.Context, communityID, state, reason string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var previous, title string
	err = tx.QueryRow(ctx, `SELECT connection_status,title FROM communities WHERE id=$1 FOR UPDATE`, communityID).Scan(&previous, &title)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE communities SET connection_status=$2,last_permission_check_at=now(),
        last_permission_error=NULLIF($3,'') WHERE id=$1`, communityID, state, reason)
	if err != nil {
		return err
	}
	if previous != state && state != "healthy" {
		rows, err := tx.Query(ctx, `SELECT cu.telegram_user_id FROM communities c JOIN creator_users cu ON cu.creator_id=c.creator_id
            JOIN workspaces w ON w.id=c.workspace_id WHERE c.id=$1 AND cu.role='owner' AND w.notifications_enabled`, communityID)
		if err != nil {
			return err
		}
		var owners []int64
		for rows.Next() {
			var owner int64
			if err := rows.Scan(&owner); err != nil {
				rows.Close()
				return err
			}
			owners = append(owners, owner)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, owner := range owners {
			message := fmt.Sprintf("MemberOS cannot manage access for %s: %s. Check the bot's administrator rights in Telegram, then recheck the connection in Studio.", title, reason)
			key := fmt.Sprintf("bot-health:%s:%s:%s", communityID, state, time.Now().UTC().Format("2006-01-02"))
			if err := notify.Queue(ctx, tx, s.BotID, owner, key, message); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
