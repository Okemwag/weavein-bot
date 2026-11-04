package memberos

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
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
		return 20
	case path == "/api/v1/checkouts":
		return 20
	case path == "/api/v1/me/support-tickets" || path == "/api/v1/me/refund-requests":
		return 10
	case path == "/api/v1/creator-inquiries":
		return 5
	case path == "/api/v1/communities" || strings.HasPrefix(path, "/api/v1/communities/"):
		return 120
	default:
		return 0
	}
}

func (a *app) withHTTPMiddleware(next http.Handler) http.Handler {
	limiter := &requestLimiter{}
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
		if limit := limitForPath(r.URL.Path); limit > 0 {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			bucketPath := r.URL.Path
			if strings.HasPrefix(bucketPath, "/api/v1/communities/") {
				bucketPath = "/api/v1/communities/*"
			}
			if !limiter.allow(ip+"|"+r.Method+"|"+bucketPath, limit, started) {
				recorder.Header().Set("Retry-After", "60")
				http.Error(recorder, "too many requests", http.StatusTooManyRequests)
				slog.Warn("http rate limited", "request_id", requestID, "path", r.URL.Path, "method", r.Method)
				return
			}
		}
		next.ServeHTTP(recorder, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/health/") {
			slog.Info("http request", "request_id", requestID, "method", r.Method, "path", r.URL.Path,
				"status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
		}
	})
}
