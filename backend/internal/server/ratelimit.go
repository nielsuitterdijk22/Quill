package server

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

// ipRateLimiter is a per-IP fixed-window rate limiter. It is safe for
// concurrent use. Entries are never evicted, but for low-traffic endpoints
// like auth the map stays small — a future improvement is a background sweep.
type ipRateLimiter struct {
	limit  int
	window time.Duration
	m      sync.Map // map[string]*rlBucket
}

type rlBucket struct {
	mu    sync.Mutex
	count int
	reset time.Time
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{limit: limit, window: window}
}

// allow returns true when the caller is within their allowance and increments
// the counter. It resets the window when the previous window has expired.
func (rl *ipRateLimiter) allow(ip string) bool {
	now := time.Now()
	v, _ := rl.m.LoadOrStore(ip, &rlBucket{reset: now.Add(rl.window)})
	b := v.(*rlBucket)
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.After(b.reset) {
		b.count = 0
		b.reset = now.Add(rl.window)
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

// middleware returns an http.Handler middleware that enforces the rate limit
// per remote IP. chi's RealIP middleware must run before this so that
// X-Forwarded-For / X-Real-IP headers are already applied to r.RemoteAddr.
func (rl *ipRateLimiter) middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "60")
				httpx.Error(w, http.StatusTooManyRequests, "rate_limited", "too many requests — please wait before trying again")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
