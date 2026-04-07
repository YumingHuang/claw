package channels

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YumingHuang/claw/internal/audit"
	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/models"
	"golang.org/x/time/rate"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type limiterStore struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	limit    rate.Limit
	burst    int
}

func newLimiterStore(cfg config.RateLimitConfig) *limiterStore {
	s := &limiterStore{
		limiters: make(map[string]*limiterEntry),
		limit:    rate.Limit(float64(cfg.RequestsPerMinute) / 60.0),
		burst:    cfg.Burst,
	}
	go s.cleanupLoop()
	return s
}

func (s *limiterStore) get(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.limiters[key]
	if !ok {
		entry = &limiterEntry{limiter: rate.NewLimiter(s.limit, s.burst)}
		s.limiters[key] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (s *limiterStore) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		cutoff := time.Now().Add(-30 * time.Minute)
		for key, entry := range s.limiters {
			if entry.lastSeen.Before(cutoff) {
				delete(s.limiters, key)
			}
		}
		s.mu.Unlock()
	}
}

func AuthMiddleware(cfg config.AuthConfig, auditor *audit.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			credential := extractAPIKey(r)
			if credential == "" || !authorized(cfg.APIKeys, credential) {
				if auditor != nil {
					auditor.LogAuthFailed(r.Context(), credential)
				}
				writeError(w, models.NewAPIError(models.ErrUnauthorized, "invalid API key"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RateLimitMiddleware(cfg config.RateLimitConfig) func(http.Handler) http.Handler {
	store := newLimiterStore(cfg)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			key := rateLimitKey(r)
			if !store.get(key).Allow() {
				w.Header().Set("Retry-After", retryAfterSeconds(cfg))
				writeError(w, models.NewAPIError(models.ErrRateLimited, "rate limit exceeded"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func verifyFeishuToken(cfg config.FeishuChannelConfig, event feishuWebhookBody) error {
	if strings.TrimSpace(cfg.VerificationToken) == "" {
		return nil
	}

	token := strings.TrimSpace(event.Token)
	if token == "" && event.Header != nil {
		token = strings.TrimSpace(event.Header.Token)
	}
	if token == "" {
		return fmt.Errorf("missing verification token")
	}
	if subtle.ConstantTimeCompare([]byte(cfg.VerificationToken), []byte(token)) != 1 {
		return fmt.Errorf("invalid verification token")
	}
	return nil
}

func authorized(allowed []string, candidate string) bool {
	for _, key := range allowed {
		if subtle.ConstantTimeCompare([]byte(key), []byte(candidate)) == 1 {
			return true
		}
	}
	return false
}

func extractAPIKey(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func rateLimitKey(r *http.Request) string {
	if key := extractAPIKey(r); key != "" {
		return "api:" + key
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return "ip:" + host
	}
	if r.RemoteAddr != "" {
		return "ip:" + r.RemoteAddr
	}
	return "ip:unknown"
}

func retryAfterSeconds(cfg config.RateLimitConfig) string {
	if cfg.RequestsPerMinute <= 0 {
		return "60"
	}
	seconds := int((time.Minute / time.Duration(cfg.RequestsPerMinute)).Seconds())
	if seconds <= 0 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
