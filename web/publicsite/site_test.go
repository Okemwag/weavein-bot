package publicsite

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHomePageLinksIntoTelegram(t *testing.T) {
	w := httptest.NewRecorder()
	Handler("MemberBot", Policies{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://t.me/MemberBot?startapp") {
		t.Fatalf("code %d", w.Code)
	}
}

func TestHomePageWorksWithoutABotUsername(t *testing.T) {
	w := httptest.NewRecorder()
	Handler("", Policies{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || strings.Contains(body, "t.me/") && strings.Contains(body, "startapp") {
		t.Fatalf("code %d", w.Code)
	}
}

func TestHomePageShowsPublishedPolicies(t *testing.T) {
	w := httptest.NewRecorder()
	p := Policies{Terms: "https://example.com/terms", Privacy: "https://example.com/privacy", Refunds: "https://example.com/refunds"}
	Handler("", p).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	for _, want := range []string{p.Terms, p.Privacy, p.Refunds} {
		if !strings.Contains(body, `href="`+want+`"`) {
			t.Fatalf("missing link %s", want)
		}
	}
	if strings.Contains(body, "paid terms will be published") {
		t.Fatal("early-access notice shown although policies are published")
	}
}
