package billing

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func AuditEvent(ctx context.Context, tx pgx.Tx, creatorID string, userID int64, action string, updateID int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(creator_id,subject_telegram_user_id,action,update_id)
        VALUES($1,$2,$3,$4)`, creatorID, userID, action, updateID)
	return err
}
