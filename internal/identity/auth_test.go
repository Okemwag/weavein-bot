package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signedInitData(token string, now time.Time) string {
	values := url.Values{
		"user": {`{"id":123456789,"first_name":"Ada"}`},
	}
	values.Set("auth_date", strconv.FormatInt(now.Unix(), 10))
	fields := []string{"auth_date=" + values.Get("auth_date"), "user=" + values.Get("user")}
	sort.Strings(fields)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	signature := hmac.New(sha256.New, secret.Sum(nil))
	signature.Write([]byte(strings.Join(fields, "\n")))
	values.Set("hash", hex.EncodeToString(signature.Sum(nil)))
	return values.Encode()
}

func TestVerifyInitData(t *testing.T) {
	now := time.Unix(1730000000, 0)
	raw := signedInitData("test-token", now)
	identity, err := VerifyInitData(raw, "test-token", now)
	if err != nil || identity.ID != 123456789 {
		t.Fatalf("valid identity: %#v, %v", identity, err)
	}
	for name, value := range map[string]string{
		"wrong bot": "test-token-2",
		"stale":     "test-token",
	} {
		checkAt := now
		if name == "stale" {
			checkAt = now.Add(6 * time.Minute)
		}
		if _, err := VerifyInitData(raw, value, checkAt); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := VerifyInitData(raw+"&user=%7B%7D", "test-token", now); err == nil {
		t.Error("duplicate field accepted")
	}
	if _, err := VerifyInitData(strings.Replace(raw, "123456789", "987654321", 1), "test-token", now); err == nil {
		t.Error("tampered user accepted")
	}
}

func TestSession(t *testing.T) {
	now := time.Unix(1730000000, 0)
	token := SignSession(123456789, "session-secret", now)
	user, err := VerifySession(token, "session-secret", now.Add(time.Minute))
	if err != nil || user != 123456789 {
		t.Fatalf("session verification: %d, %v", user, err)
	}
	if _, err := VerifySession(token, "wrong-secret", now); err == nil {
		t.Error("wrong key accepted")
	}
	if _, err := VerifySession(token, "session-secret", now.Add(31*time.Minute)); err == nil {
		t.Error("expired session accepted")
	}
}
