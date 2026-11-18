package notify

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// Queue records a message only after the subscriber has contacted this bot in
// a private chat. The unique key protects against replayed webhook events.
func Queue(ctx context.Context, tx pgx.Tx, botID, userID int64, key, message string) error {
	tag, err := tx.Exec(ctx, `INSERT INTO notifications(bot_id,telegram_user_id,dedupe_key,message)
        SELECT $1::bigint,$2::bigint,$3::text,$4::text FROM bot_private_contacts
        WHERE bot_id=$1 AND telegram_user_id=$2
        ON CONFLICT (dedupe_key) DO NOTHING`, botID, userID, key, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var contactable bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot_private_contacts WHERE bot_id=$1 AND telegram_user_id=$2)`, botID, userID).Scan(&contactable); err != nil {
			return err
		}
		if !contactable {
			slog.Warn("notification dropped: subscriber has not allowed bot messages", "user_id", userID, "dedupe_key", key)
		}
	}
	return nil
}
