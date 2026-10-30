package ctxdata

import "context"

type userIdKey struct{}

// WithUserId stores the authenticated user id in ctx.
func WithUserId(ctx context.Context, userId uint64) context.Context {
	return context.WithValue(ctx, userIdKey{}, userId)
}

// UserIdFromCtx returns the authenticated user id stored by the auth middleware.
func UserIdFromCtx(ctx context.Context) (uint64, bool) {
	userId, ok := ctx.Value(userIdKey{}).(uint64)
	return userId, ok
}
