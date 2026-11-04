package notify

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Queue records a message only after the subscriber has contacted this bot in
// a private chat. The unique key protects against replayed webhook events.
func Queue(ctx context.Context, tx pgx.Tx, botID, userID int64, key, message string) error {
	_, err := tx.Exec(ctx, `INSERT INTO notifications(bot_id,telegram_user_id,dedupe_key,message)
        SELECT $1,$2,$3,$4 FROM bot_private_contacts
        WHERE bot_id=$1 AND telegram_user_id=$2
        ON CONFLICT (dedupe_key) DO NOTHING`, botID, userID, key, message)
	return err
}
