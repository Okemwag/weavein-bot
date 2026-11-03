package access

import (
	"memberos/internal/telegram"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	DB       *pgxpool.Pool
	Telegram telegram.Client
	BotID    int64
}
