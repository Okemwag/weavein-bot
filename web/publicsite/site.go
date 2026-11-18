package publicsite

import (
	"embed"
	"html/template"
	"net/http"
	"regexp"
)

//go:embed index.html site.css site.js policy.html terms.html privacy.html refunds.html
var assets embed.FS

// PolicyEffectiveDate is shown on every built-in policy page. Change it whenever
// the wording of terms.html, privacy.html or refunds.html changes.
const PolicyEffectiveDate = "18 November 2026"

var home = template.Must(template.ParseFS(assets, "index.html"))
var policyPages = map[string]*template.Template{
	"/terms":   template.Must(template.ParseFS(assets, "policy.html", "terms.html")),
	"/privacy": template.Must(template.ParseFS(assets, "policy.html", "privacy.html")),
	"/refunds": template.Must(template.ParseFS(assets, "policy.html", "refunds.html")),
}
var botName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)

// Policies holds operator-published legal document URLs. Empty fields are not shown.
type Policies struct {
	Terms   string `json:"terms_url,omitempty"`
	Privacy string `json:"privacy_url,omitempty"`
	Refunds string `json:"refund_policy_url,omitempty"`
}

// Complete reports whether every buyer-facing policy is published.
func (p Policies) Complete() bool { return p.Terms != "" && p.Privacy != "" && p.Refunds != "" }

// Operator identifies who runs this MemberOS deployment. The built-in policy
// pages are served only when it is set, because a policy needs a named party.
type Operator struct {
	LegalName string
	Email     string
	Address   string
	Country   string
}

// Set reports whether the operator details needed by the policies are present.
func (o Operator) Set() bool { return o.LegalName != "" && o.Email != "" && o.Country != "" }

// Pricing describes MemberOS's own plans as configured for this deployment.
type Pricing struct {
	ProAvailable   bool
	ProStars       int
	ProCommunities int
	GraceDays      int
}

// Site is everything the public pages need from configuration.
type Site struct {
	BotUsername string
	Policies    Policies
	Operator    Operator
	Pricing     Pricing
}

func Handler(site Site) http.Handler {
	botURL := ""
	if botName.MatchString(site.BotUsername) {
		botURL = "https://t.me/" + site.BotUsername + "?startapp"
	}
	data := map[string]any{"BotURL": botURL, "Policies": site.Policies, "Operator": site.Operator,
		"Pricing": site.Pricing, "Effective": PolicyEffectiveDate}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'")
		if page, ok := policyPages[r.URL.Path]; ok {
			if !site.Operator.Set() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = page.ExecuteTemplate(w, "policy.html", data)
			return
		}
		switch r.URL.Path {
		case "/", "/pricing", "/support":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = home.Execute(w, data)
		case "/site.css", "/site.js":
			if r.URL.Path == "/site.css" {
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
			} else {
				w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			}
			body, err := assets.ReadFile(r.URL.Path[1:])
			if err != nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	})
}
