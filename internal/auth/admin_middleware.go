package auth

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultAdminRateLimit = 100
	defaultAdminBurst     = 20
)

// AdminMiddlewareConfig configures the admin authentication middleware.
type AdminMiddlewareConfig struct {
	MasterKeys []string
	Logger     *log.Logger
	RateLimit  rate.Limit
	Burst      int
}

func AdminAuthMiddleware(cfg AdminMiddlewareConfig) func(http.Handler) http.Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	masterSet := make(map[string]struct{}, len(cfg.MasterKeys))
	for _, key := range cfg.MasterKeys {
		masterSet[key] = struct{}{}
	}

	limit := cfg.RateLimit
	if limit == 0 {
		limit = rate.Every(time.Minute / defaultAdminRateLimit)
	}
	burst := cfg.Burst
	if burst == 0 {
		burst = defaultAdminBurst
	}

	ipLimiter := newIPRateLimiter(limit, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !ipLimiter.Allow(ip) {
				logger.Printf("WARN: admin rate limit exceeded ip=%s path=%s", ip, r.URL.Path)
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				return
			}

			adminKey := r.Header.Get(adminKeyHeader)
			if adminKey == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			if _, ok := masterSet[adminKey]; !ok {
				logger.Printf("WARN: invalid admin key ip=%s path=%s", ip, r.URL.Path)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			ctx := withAdminOperator(r.Context(), adminKey)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	limit    rate.Limit
	burst    int
	ttl      time.Duration
}

type rateLimiterEntry struct {
	limiter *rate.Limiter
	expires time.Time
}

func newIPRateLimiter(limit rate.Limit, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		limit:    limit,
		burst:    burst,
		ttl:      10 * time.Minute,
	}
}

func (l *ipRateLimiter) Allow(ip string) bool {
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.limiters[ip]
	if !ok || now.After(entry.expires) {
		entry = &rateLimiterEntry{
			limiter: rate.NewLimiter(l.limit, l.burst),
		}
		l.limiters[ip] = entry
	}
	entry.expires = now.Add(l.ttl)
	return entry.limiter.Allow()
}

func clientIP(r *http.Request) string {
	if header := r.Header.Get("X-Forwarded-For"); header != "" {
		parts := strings.Split(header, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
