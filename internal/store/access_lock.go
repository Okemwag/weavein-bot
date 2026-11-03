package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LockAccess serializes membership changes for one community and Telegram user.
func LockAccess(ctx context.Context, tx pgx.Tx, communityID string, userID int64) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("%s:%d", communityID, userID))
	return err
}
