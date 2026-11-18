package memberos

import (
	"errors"
	"fmt"
	"memberos/internal/telegram"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cases := []struct {
		name, remote, forwarded, want string
		trusted                       []netip.Prefix
	}{
		{"no proxy configured ignores header", "203.0.113.9:1", "198.51.100.1", "203.0.113.9", nil},
		{"untrusted peer cannot spoof", "203.0.113.9:1", "198.51.100.1", "203.0.113.9", proxies},
		{"trusted proxy gives real client", "10.0.0.1:1", "198.51.100.1", "198.51.100.1", proxies},
		{"client-supplied prefix is not believed", "10.0.0.1:1", "1.1.1.1, 198.51.100.1", "198.51.100.1", proxies},
		{"proxy chain is skipped", "10.0.0.1:1", "198.51.100.1, 10.0.0.2", "198.51.100.1", proxies},
		{"garbage falls back to peer", "10.0.0.1:1", "not-an-ip", "10.0.0.1", proxies},
		{"missing header falls back to peer", "10.0.0.1:1", "", "10.0.0.1", proxies},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tc.remote
		if tc.forwarded != "" {
			r.Header.Set("X-Forwarded-For", tc.forwarded)
		}
		if got := clientIP(r, tc.trusted); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
}

func TestClientsBehindProxyHaveSeparateLimits(t *testing.T) {
	a := &app{cfg: config{sessionSecret: strings.Repeat("s", 32), trustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}}
	handler := a.withHTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	send := func(client string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/creator-inquiries", nil)
		r.RemoteAddr = "10.0.0.1:443"
		r.Header.Set("X-Forwarded-For", client)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 25; i++ {
		if code := send(fmt.Sprintf("198.51.100.%d", i)); code != http.StatusOK {
			t.Fatalf("client %d limited with %d", i, code)
		}
	}
	for i := 0; i < 5; i++ {
		send("203.0.113.1")
	}
	if code := send("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("a single client must still be limited, got %d", code)
	}
}

func TestProtectedAreasRequireASession(t *testing.T) {
	a := &app{cfg: config{sessionSecret: strings.Repeat("s", 32), adminTelegramID: 7}}
	reached := false
	handler := a.withHTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	for path, want := range map[string]int{
		"/api/v1/admin/some-new-route":   http.StatusForbidden,
		"/api/v1/creator/some-new-route": http.StatusUnauthorized,
		"/api/v1/me/some-new-route":      http.StatusUnauthorized,
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want || reached {
			t.Errorf("%s: got %d reached=%v", path, w.Code, reached)
		}
	}
}

func TestMalformedIDIs404(t *testing.T) {
	a := &app{}
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/communities/not-a-uuid", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d", w.Code)
	}
}

func TestBotCommand(t *testing.T) {
	for text, want := range map[string]string{
		"/start": "/start", "/start c_abc": "/start", "/start@MemberBot": "/start", "/help@memberbot x": "/help",
		"/start@OtherBot": "", "hello": "", "/unknown": "", "": "",
	} {
		if got := botCommand(text, "MemberBot"); got != want {
			t.Errorf("%q: got %q want %q", text, got, want)
		}
	}
}

func TestClassifySendFailure(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		want  string
		delay int
	}{
		{"blocked user needs no review", telegram.StatusError{Code: 403}, "undeliverable", 0},
		{"rate limit honours retry_after", telegram.StatusError{Code: 429, RetryAfter: 77}, "pending", 77},
		{"never connected retries itself", telegram.RequestError{Unsent: true}, "pending", 15},
		{"lost response needs review", telegram.RequestError{}, "uncertain", 0},
		{"server error needs review", telegram.StatusError{Code: 502}, "uncertain", 0},
		{"unconfirmed send needs review", errors.New("no message id"), "uncertain", 0},
	}
	for _, tc := range cases {
		status, _, delay := classifySendFailure(tc.err, 0)
		if status != tc.want || delay != tc.delay {
			t.Errorf("%s: got %s/%d want %s/%d", tc.name, status, delay, tc.want, tc.delay)
		}
	}
	if status, _, _ := classifySendFailure(telegram.RequestError{Unsent: true}, 7); status != "failed" {
		t.Errorf("exhausted retries must fail, got %s", status)
	}
	if got := (telegram.StatusError{}).RetryDelay(); got != 30*time.Second {
		t.Errorf("default retry delay %v", got)
	}
}

func TestValidUUID(t *testing.T) {
	if !validUUID("db5afea2-11e4-45ff-913d-81a30ecb19be") {
		t.Error("canonical UUID rejected")
	}
	for _, bad := range []string{"", "x", "db5afea2-11e4-45ff-913d-81a30ecb19bg", "db5afea211e445ff913d81a30ecb19be", "db5afea2-11e4-45ff-913d-81a30ecb19be'"} {
		if validUUID(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
