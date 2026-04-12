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
		if !fixedLenEqual(key, s.config.Server.APIKey) {
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
	key := []byte("gopaw-auth-comparison")
	ha := hmac.New(sha256.New, key)
	ha.Write([]byte(a))
	hb := hmac.New(sha256.New, key)
	hb.Write([]byte(b))
	return hmac.Equal(ha.Sum(nil), hb.Sum(nil))
}

// corsMiddleware sets permissive CORS headers and short-circuits OPTIONS preflight.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
