package miniapp

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.css app.js shared.js subscriber.js creator.js
var assets embed.FS

func Handler() http.Handler {
	files, _ := fs.Sub(assets, ".")
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' https://telegram.org; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'")
		server.ServeHTTP(w, r)
	})
}
