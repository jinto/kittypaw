package ratelimit

import (
	"net"
	"net/http"
	"strconv"
)

const (
	AnonLimitPerMin  = 5
	GlobalDailyLimit = 10000
)

// Middleware creates anonymous IP-based rate-limit middleware for the data
// API. User identity is owned by apps/portal; this service currently accepts
// unauthenticated resource requests and protects upstreams by peer bucket.
func Middleware(limiter *Limiter, bucketPrefix ...string) func(http.Handler) http.Handler {
	prefix := ""
	if len(bucketPrefix) > 0 && bucketPrefix[0] != "" {
		prefix = bucketPrefix[0] + ":"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.AllowDaily("global", GlobalDailyLimit) {
				retryAfter := limiter.SecondsUntilDailyReset("global")
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				http.Error(w, "daily limit exceeded", http.StatusTooManyRequests)
				return
			}

			key := prefix + "ip:" + realIP(r)
			if !limiter.Allow(key, AnonLimitPerMin) {
				retryAfter := limiter.SecondsUntilReset(key)
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(AnonLimitPerMin))
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// realIP returns the IP to key anonymous rate limits on. Only X-Real-IP is
// trusted because nginx canonically overrides it with the actual TCP peer.
// True-Client-IP and X-Forwarded-For are not trusted client identity.
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
