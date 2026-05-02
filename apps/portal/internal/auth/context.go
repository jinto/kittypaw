package auth

import (
	"context"

	"github.com/kittypaw-app/kittyportal/internal/model"
)

type contextKey int

const userKey contextKey = 0

func ContextWithUser(ctx context.Context, user *model.User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

func UserFromContext(ctx context.Context) *model.User {
	u, _ := ctx.Value(userKey).(*model.User)
	return u
}
