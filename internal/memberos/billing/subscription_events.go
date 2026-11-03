package billing

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func recordSubscriptionEvent(ctx context.Context, tx pgx.Tx, subscriptionID, key, kind string, details any) error {
	_, err := tx.Exec(ctx, `INSERT INTO subscription_events(subscription_id,event_key,kind,details)
        VALUES($1,$2,$3,$4) ON CONFLICT (event_key) DO NOTHING`, subscriptionID, key, kind, details)
	return err
}
