package publicsite

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHomePageLinksIntoTelegram(t *testing.T) {
	w := httptest.NewRecorder()
	Handler(Site{BotUsername: "MemberBot"}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://t.me/MemberBot?startapp") {
		t.Fatalf("code %d", w.Code)
	}
}

func TestHomePageWorksWithoutABotUsername(t *testing.T) {
	w := httptest.NewRecorder()
	Handler(Site{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || strings.Contains(body, "t.me/") && strings.Contains(body, "startapp") {
		t.Fatalf("code %d", w.Code)
	}
}

func TestHomePageShowsPublishedPolicies(t *testing.T) {
	w := httptest.NewRecorder()
	p := Policies{Terms: "https://example.com/terms", Privacy: "https://example.com/privacy", Refunds: "https://example.com/refunds"}
	Handler(Site{Policies: p}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
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

func get(t *testing.T, site Site, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	Handler(site).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestPolicyPagesNeedANamedOperator(t *testing.T) {
	for _, path := range []string{"/terms", "/privacy", "/refunds"} {
		if code := get(t, Site{}, path).Code; code != http.StatusNotFound {
			t.Fatalf("%s served without operator: %d", path, code)
		}
	}
}

func TestPolicyPagesNameTheOperatorAndPlan(t *testing.T) {
	site := Site{Operator: Operator{LegalName: "Example Ltd", Email: "help@example.com", Country: "Kenya"},
		Pricing: Pricing{ProAvailable: true, ProStars: 750, ProCommunities: 5, GraceDays: 7}}
	for _, path := range []string{"/terms", "/privacy", "/refunds"} {
		w := get(t, site, path)
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(body, "Example Ltd") || !strings.Contains(body, "mailto:help@example.com") ||
			!strings.Contains(body, PolicyEffectiveDate) || strings.Contains(body, "{{") {
			t.Fatalf("%s: code %d", path, w.Code)
		}
	}
	if !strings.Contains(get(t, site, "/terms").Body.String(), "750 Telegram Stars every 30 days") {
		t.Fatal("terms do not state the configured Pro price")
	}
}

func TestPricingShowsConfiguredPro(t *testing.T) {
	body := get(t, Site{Pricing: Pricing{ProAvailable: true, ProStars: 750, ProCommunities: 5, GraceDays: 7}}, "/").Body.String()
	if !strings.Contains(body, "750 Stars") || !strings.Contains(body, "Up to 5 communities") {
		t.Fatal("pricing section missing Pro plan")
	}
	if strings.Contains(get(t, Site{}, "/").Body.String(), "750 Stars") {
		t.Fatal("Pro shown although SaaS billing is off")
	}
}
