package memberos

import (
	"encoding/json"
	"net/http"
)

func (a *app) notificationPreferences(w http.ResponseWriter, r *http.Request) {
	user, err := a.sessionUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodPatch {
		var input struct {
			RemindersEnabled *bool `json:"reminders_enabled"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || input.RemindersEnabled == nil {
			http.Error(w, "invalid preferences", http.StatusBadRequest)
			return
		}
		_, err = a.db.Exec(r.Context(), `INSERT INTO subscriber_notification_preferences(telegram_user_id,reminders_enabled)
            VALUES($1,$2) ON CONFLICT (telegram_user_id) DO UPDATE
            SET reminders_enabled=EXCLUDED.reminders_enabled,updated_at=now()`, user, *input.RemindersEnabled)
		if err != nil {
			http.Error(w, "database error", 500)
			return
		}
	}
	var enabled bool
	err = a.db.QueryRow(r.Context(), `SELECT coalesce((SELECT reminders_enabled FROM subscriber_notification_preferences
        WHERE telegram_user_id=$1),true)`, user).Scan(&enabled)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"reminders_enabled": enabled, "transactional_notices": "sent only after private bot contact"})
}
