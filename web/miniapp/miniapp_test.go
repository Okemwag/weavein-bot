package miniapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every script on disk must be embedded and every import must resolve, or the
// whole app fails to start with nothing but a blank screen.
func TestAllScriptsAreServedAndImportsResolve(t *testing.T) {
	handler := Handler()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+path, nil))
		return w
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	imports := regexp.MustCompile(`from '\./([a-z_]+\.js)'`)
	checked := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}
		checked++
		response := get(entry.Name())
		if response.Code != http.StatusOK {
			t.Errorf("%s is not embedded: %d", entry.Name(), response.Code)
			continue
		}
		for _, match := range imports.FindAllStringSubmatch(response.Body.String(), -1) {
			if get(match[1]).Code != http.StatusOK {
				t.Errorf("%s imports %s, which is not served", entry.Name(), match[1])
			}
		}
	}
	if checked < 10 {
		t.Fatalf("found only %d scripts", checked)
	}
}

func TestPolicyBlocksInlineContent(t *testing.T) {
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	policy := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "default-src 'none'") || strings.Contains(policy, "unsafe-inline") {
		t.Fatalf("policy %q", policy)
	}
}
