package ratelimit

import (
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

func realIP(r *http.Request) string {
	// Chi's RealIP middleware already sets RemoteAddr.
	return r.RemoteAddr
}
