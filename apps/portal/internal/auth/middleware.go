package auth

import (
	"net/http"
	"strings"

	"github.com/kittypaw-app/kittyportal/internal/model"
)

// Middleware verifies the Bearer JWT and injects the resolved User into
// request context. Anonymous (no Authorization header) passes through
// with nil user.
//
// Plan 21 PR-B: HS256 secret replaced with JWKSProvider. audience is
// caller-pinned — user middleware passes AudienceAPI, future device
// middleware will pass AudienceChat. The cross-audience leak guard
// lives in Verify.
func Middleware(jwks JWKSProvider, audience string, users model.UserStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				// Anonymous — nil user in context.
				next.ServeHTTP(w, r)
				return
			}

			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "invalid authorization header", http.StatusUnauthorized)
				return
			}

			claims, err := Verify(parts[1], jwks, audience)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			user, err := users.FindByID(r.Context(), claims.UserID)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			ctx := ContextWithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
