package memberos

import (
	"encoding/json"
	"errors"
	"memberos/internal/identity"
	"net/http"
	"strings"
	"time"
)

func (a *app) session(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	user, err := identity.VerifyInitData(body.InitData, a.cfg.botToken, time.Now())
	if err != nil {
		http.Error(w, "invalid Telegram identity", http.StatusUnauthorized)
		return
	}
	if _, err := a.db.Exec(r.Context(), `INSERT INTO telegram_profiles(telegram_user_id,first_name,username)
        VALUES($1,$2,$3) ON CONFLICT (telegram_user_id) DO UPDATE SET first_name=EXCLUDED.first_name,
        username=EXCLUDED.username,verified_at=now()`, user.ID, user.FirstName, user.Username); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE members SET first_name=$2,username=$3,verified_at=now()
        WHERE telegram_user_id=$1`, user.ID, user.FirstName, user.Username); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": identity.SignSession(user.ID, a.cfg.sessionSecret, time.Now()), "expires_in": 1800,
		"telegram_user_id": user.ID, "is_operator": a.cfg.adminTelegramID > 0 && user.ID == a.cfg.adminTelegramID})
}

func (a *app) sessionUser(r *http.Request) (int64, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return 0, errors.New("missing bearer session")
	}
	return identity.VerifySession(strings.TrimPrefix(header, "Bearer "), a.cfg.sessionSecret, time.Now())
}
