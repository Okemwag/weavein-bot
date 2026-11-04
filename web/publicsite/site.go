package publicsite

import (
	"embed"
	"html/template"
	"net/http"
	"regexp"
)

//go:embed index.html site.css site.js
var assets embed.FS

var home = template.Must(template.ParseFS(assets, "index.html"))
var botName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)

func Handler(username string) http.Handler {
	botURL := ""
	if botName.MatchString(username) {
		botURL = "https://t.me/" + username + "?startapp"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'")
		switch r.URL.Path {
		case "/", "/pricing", "/support":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = home.Execute(w, map[string]any{"BotURL": botURL})
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
