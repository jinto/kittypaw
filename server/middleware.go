package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"net/http"
	"strings"
)

// requireAPIKey validates Bearer token or x-api-key header.
// Uses constant-time comparison to prevent timing attacks.
func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		if key == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				key = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		apiKey := s.effectiveAPIKey()
		if !fixedLenEqual(key, apiKey) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// fixedLenEqual compares two strings in constant time using HMAC.
// Both inputs are hashed with a fixed key so comparison time does not
// leak information about either value's length or content.
// Returns false if either string is empty to prevent auth bypass.
func fixedLenEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	key := []byte("kittypaw-auth-comparison")
	ha := hmac.New(sha256.New, key)
	ha.Write([]byte(a))
	hb := hmac.New(sha256.New, key)
	hb.Write([]byte(b))
	return hmac.Equal(ha.Sum(nil), hb.Sum(nil))
}

// corsMiddleware reads AllowedOrigins from config on every request so that
// hot-reloads via /api/v1/reload take effect without a server restart.
// When no origins are configured, all origins are permitted (dev mode).
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			s.configMu.RLock()
			allowedOrigins := s.config.Server.AllowedOrigins
			s.configMu.RUnlock()

			allowed := len(allowedOrigins) == 0
			for _, o := range allowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")
				w.Header().Set("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				if !allowed {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
