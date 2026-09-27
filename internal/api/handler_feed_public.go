package api

import (
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Resinat/Resin/internal/feed"
	"github.com/Resinat/Resin/internal/service"
)

func HandlePublicFeed(cp *service.ControlPlaneService) http.HandlerFunc {
	return handlePublicFeedWithLimiter(cp, newPublicFeedRateLimiter(120, time.Minute))
}

// handlePublicFeedWithLimiter serves bearer-token subscription URLs. The
// limiter is injected so the server can share one limiter across the four
// route patterns and tests can use a small deterministic limit.
func handlePublicFeedWithLimiter(cp *service.ControlPlaneService, limiter *publicFeedRateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(PathParam(r, "token"))
		requested := strings.TrimSpace(PathParam(r, "format"))
		if limiter != nil {
			if allowed, retryAfter := limiter.allow(r, token); !allowed {
				w.Header().Set("Retry-After", itoa(retryAfter))
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
		}
		_, result, etag, err := cp.RenderFeed(token, requested)
		if err != nil {
			// Public token lookup intentionally hides whether the feed exists,
			// is disabled, or has an unsupported format.
			if svc, ok := err.(*service.ServiceError); ok && svc.Code == "NOT_FOUND" {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "feed unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=30")
		w.Header().Set("ETag", etag)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("X-Resin-Node-Count", itoa(result.NodeCount))
		w.Header().Set("X-Resin-Skipped-Count", itoa(result.SkippedCount))
		if len(result.SkippedTypes) > 0 {
			w.Header().Set("X-Resin-Skipped-Types", strings.Join(result.SkippedTypes, ","))
		}
		w.Header().Set("Content-Type", result.ContentType)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.Body)
	}
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

type publicFeedRateLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	buckets   map[string]publicFeedBucket
	lastClean time.Time
}

type publicFeedBucket struct {
	started time.Time
	count   int
}

func newPublicFeedRateLimiter(limit int, window time.Duration) *publicFeedRateLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}
	return &publicFeedRateLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]publicFeedBucket),
	}
}

func (l *publicFeedRateLimiter) allow(r *http.Request, token string) (bool, int) {
	if l == nil {
		return true, 0
	}
	now := time.Now()
	client := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if client == "" {
		client = r.RemoteAddr
		if host, _, err := net.SplitHostPort(client); err == nil {
			client = host
		}
	}
	if client == "" {
		client = "unknown"
	}
	keys := []string{"ip:" + client}
	if token != "" {
		keys = append(keys, "token:"+feed.HashToken(token))
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastClean.IsZero() || now.Sub(l.lastClean) >= l.window {
		for key, bucket := range l.buckets {
			if now.Sub(bucket.started) >= l.window {
				delete(l.buckets, key)
			}
		}
		l.lastClean = now
	}
	retryAfter := 0
	exhausted := false
	incremented := make([]string, 0, len(keys))
	for _, key := range keys {
		bucket := l.buckets[key]
		if bucket.started.IsZero() || now.Sub(bucket.started) >= l.window {
			bucket = publicFeedBucket{started: now}
		}
		if bucket.count >= l.limit {
			exhausted = true
			remaining := l.window - now.Sub(bucket.started)
			seconds := int(math.Ceil(remaining.Seconds()))
			if seconds > retryAfter {
				retryAfter = seconds
			}
			l.buckets[key] = bucket
			continue
		}
		bucket.count++
		l.buckets[key] = bucket
		incremented = append(incremented, key)
	}
	// A request rejected by one dimension must not consume the other dimension.
	if exhausted {
		for _, key := range incremented {
			bucket := l.buckets[key]
			if bucket.count > 0 {
				bucket.count--
				l.buckets[key] = bucket
			}
		}
		return false, retryAfter
	}
	return true, 0
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if i == len(buf) {
		return "0"
	}
	return string(buf[i:])
}
