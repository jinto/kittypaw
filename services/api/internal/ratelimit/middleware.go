package ratelimit

import (
	"net"
	"net/http"
	"strconv"

	"github.com/kittypaw-app/kittyapi/internal/auth"
)

const (
	AnonLimitPerMin  = 5
	AuthLimitPerMin  = 60
	GlobalDailyLimit = 10000
)

func Middleware(limiter *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Global daily limit.
			if !limiter.AllowDaily("global", GlobalDailyLimit) {
				retryAfter := limiter.SecondsUntilDailyReset("global")
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				http.Error(w, "daily limit exceeded", http.StatusTooManyRequests)
				return
			}

			user := auth.UserFromContext(r.Context())

			var key string
			var limit int
			if user != nil {
				key = "user:" + user.ID
				limit = AuthLimitPerMin
			} else {
				key = "ip:" + realIP(r)
				limit = AnonLimitPerMin
			}

			if !limiter.Allow(key, limit) {
				retryAfter := limiter.SecondsUntilReset(key)
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// realIP returns the IP to key anonymous rate limits on. Only X-Real-IP is
// trusted — nginx canonically overrides this header with the actual TCP
// peer (`proxy_set_header X-Real-IP $remote_addr;` in the standard
// proxy_params snippet). True-Client-IP and X-Forwarded-For are NOT
// consulted: the same proxy_params either leaves True-Client-IP untouched
// or *appends* the real peer to the end of X-Forwarded-For, both of which
// leave attacker-supplied values at the head of the header. Trusting them
// would let any caller rotate the rate-limit key per request.
//
// Without a reverse proxy (X-Real-IP unset), falls back to the host
// portion of r.RemoteAddr (Go's TCP peer).
func realIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
