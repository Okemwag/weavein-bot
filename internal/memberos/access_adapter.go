package memberos

import "memberos/internal/memberos/access"

func (a *app) accessService() access.Service {
	return access.Service{DB: a.db, Telegram: a.telegram, BotID: a.cfg.botID, AppOrigin: a.cfg.publicWebhookURL}
}
