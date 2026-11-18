package publicsite

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHomePageLinksIntoTelegram(t *testing.T) {
	w := httptest.NewRecorder()
	Handler("MemberBot").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://t.me/MemberBot?startapp") {
		t.Fatalf("code %d", w.Code)
	}
}

func TestHomePageWorksWithoutABotUsername(t *testing.T) {
	w := httptest.NewRecorder()
	Handler("").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || strings.Contains(body, "t.me/") && strings.Contains(body, "startapp") {
		t.Fatalf("code %d", w.Code)
	}
}
