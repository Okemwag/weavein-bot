package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TelegramIdentity struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
	// AllowsWriteToPM is Telegram's signed statement that the bot may message this user.
	AllowsWriteToPM bool `json:"allows_write_to_pm"`
}

func VerifyInitData(raw, token string, now time.Time) (TelegramIdentity, error) {
	var user TelegramIdentity
	if len(raw) == 0 || len(raw) > 8192 {
		return user, errors.New("invalid initData size")
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return user, err
	}
	fields := make([]string, 0, len(values))
	var givenHash string
	for key, items := range values {
		if len(items) != 1 {
			return user, errors.New("duplicate initData field")
		}
		if key == "hash" {
			givenHash = items[0]
			continue
		}
		fields = append(fields, key+"="+items[0])
	}
	received, err := hex.DecodeString(givenHash)
	if err != nil || len(received) != sha256.Size {
		return user, errors.New("invalid initData hash")
	}
	sort.Strings(fields)
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretMAC.Write([]byte(token))
	checkMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	checkMAC.Write([]byte(strings.Join(fields, "\n")))
	if !hmac.Equal(received, checkMAC.Sum(nil)) {
		return user, errors.New("invalid initData signature")
	}
	issued, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return user, errors.New("missing auth_date")
	}
	timestamp := time.Unix(issued, 0)
	if timestamp.Before(now.Add(-5*time.Minute)) || timestamp.After(now.Add(time.Minute)) {
		return user, errors.New("stale initData")
	}
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID <= 0 {
		return TelegramIdentity{}, errors.New("missing Telegram user")
	}
	return user, nil
}

func SignSession(userID int64, key string, now time.Time) string {
	payload := fmt.Sprintf("%d:%d", userID, now.Add(30*time.Minute).Unix())
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func VerifySession(value, key string, now time.Time) (int64, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return 0, errors.New("invalid session")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, errors.New("invalid session")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, errors.New("invalid session")
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, errors.New("invalid session")
	}
	idAndExpiry := strings.Split(string(payload), ":")
	if len(idAndExpiry) != 2 {
		return 0, errors.New("invalid session")
	}
	id, idErr := strconv.ParseInt(idAndExpiry[0], 10, 64)
	expiry, expiryErr := strconv.ParseInt(idAndExpiry[1], 10, 64)
	if idErr != nil || expiryErr != nil || id <= 0 || now.Unix() >= expiry {
		return 0, errors.New("expired session")
	}
	return id, nil
}
