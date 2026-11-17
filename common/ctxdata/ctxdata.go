package ctxdata

import "context"

type userIdKey struct{}

// WithUserId stores the authenticated user id in ctx.
// 往context赛一组kv，
func WithUserId(ctx context.Context, userId uint64) context.Context {
	return context.WithValue(ctx, userIdKey{}, userId)
}

// UserIdFromCtx returns the authenticated user id stored by the auth middleware.
func UserIdFromCtx(ctx context.Context) (uint64, bool) {
	// 从上下文把之前赛进入的值拿出来
	userId, ok := ctx.Value(userIdKey{}).(uint64)
	return userId, ok
}
