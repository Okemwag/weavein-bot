package memberos

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

type responseStatus struct {
	http.ResponseWriter
	status int
}

func (w *responseStatus) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }

type requestBucket struct {
	count int
	reset time.Time
}
type requestLimiter struct {
	mu      sync.Mutex
	buckets map[string]requestBucket
}

func (l *requestLimiter) allow(key string, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = make(map[string]requestBucket)
	}
	if len(l.buckets) >= 10000 {
		for k, v := range l.buckets {
			if !v.reset.After(now) {
				delete(l.buckets, k)
			}
		}
	}
	if len(l.buckets) >= 10000 {
		if _, exists := l.buckets[key]; !exists {
			return false
		}
	}
	b := l.buckets[key]
	if !b.reset.After(now) {
		b = requestBucket{reset: now.Add(time.Minute)}
	}
	if b.count >= limit {
		l.buckets[key] = b
		return false
	}
	b.count++
	l.buckets[key] = b
	return true
}

func limitForPath(path string) int {
	switch {
	case path == "/api/v1/telegram/session":
		return 60
	case path == "/api/v1/checkouts":
		return 20
	case path == "/api/v1/me/support-tickets" || path == "/api/v1/me/refund-requests":
		return 10
	case path == "/api/v1/me/membership":
		return 60
	case path == "/api/v1/creator-inquiries":
		return 5
	case path == "/api/v1/communities" || strings.HasPrefix(path, "/api/v1/communities/"):
		return 120
	default:
		return 0
	}
}

// clientIP walks X-Forwarded-For from the nearest hop, skipping configured
// proxies. The header is only believed when the connection itself comes from a
// trusted proxy, because anyone else can send whatever value they like.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	isTrusted := func(value string) bool {
		addr, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil {
			return false
		}
		for _, prefix := range trusted {
			if prefix.Contains(addr.Unmap()) {
				return true
			}
		}
		return false
	}
	if !isTrusted(host) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if _, err := netip.ParseAddr(hop); err != nil {
			break
		}
		if !isTrusted(hop) {
			return hop
		}
	}
	return host
}

// rateLimitKey counts signed-in users individually, so people behind one mobile
// carrier address do not share a budget. Everyone else is counted by client IP.
func (a *app) rateLimitKey(r *http.Request) string {
	if user, err := a.sessionUser(r); err == nil {
		return "user:" + strconv.FormatInt(user, 10)
	}
	return "ip:" + clientIP(r, a.cfg.trustedProxies)
}

// requireSessionForArea is a backstop: a handler added under these prefixes
// cannot become public by forgetting its own check. Handlers still enforce
// tenant ownership themselves.
func (a *app) requireSessionForArea(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/api/v1/admin/"):
		user, err := a.sessionUser(r)
		if err != nil || a.cfg.adminTelegramID == 0 || user != a.cfg.adminTelegramID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
	case strings.HasPrefix(path, "/api/v1/creator/"), strings.HasPrefix(path, "/api/v1/me/"):
		if _, err := a.sessionUser(r); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
	}
	return true
}

func (a *app) withHTTPMiddleware(next http.Handler) http.Handler {
	limiter := &requestLimiter{}
	var warnProxy sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var token [12]byte
		if _, err := rand.Read(token[:]); err != nil {
			http.Error(w, "request unavailable", http.StatusServiceUnavailable)
			return
		}
		requestID := hex.EncodeToString(token[:])
		w.Header().Set("X-Request-ID", requestID)
		recorder := &responseStatus{ResponseWriter: w, status: http.StatusOK}
		if len(a.cfg.trustedProxies) == 0 && r.Header.Get("X-Forwarded-For") != "" {
			warnProxy.Do(func() {
				slog.Warn("requests arrive through a proxy but TRUSTED_PROXY_CIDRS is unset; anonymous clients share one rate limit")
			})
		}
		if limit := limitForPath(r.URL.Path); limit > 0 {
			bucketPath := r.URL.Path
			if strings.HasPrefix(bucketPath, "/api/v1/communities/") {
				bucketPath = "/api/v1/communities/*"
			}
			if !limiter.allow(a.rateLimitKey(r)+"|"+r.Method+"|"+bucketPath, limit, started) {
				recorder.Header().Set("Retry-After", "60")
				http.Error(recorder, "too many requests", http.StatusTooManyRequests)
				slog.Warn("http rate limited", "request_id", requestID, "path", r.URL.Path, "method", r.Method)
				return
			}
		}
		if a.requireSessionForArea(recorder, r) {
			next.ServeHTTP(recorder, r)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/health/") {
			slog.Info("http request", "request_id", requestID, "method", r.Method, "path", r.URL.Path,
				"status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
		}
	})
}
