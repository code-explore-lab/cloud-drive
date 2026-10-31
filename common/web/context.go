// Package web 提供所有 HTTP 服务共用的横切能力：请求 ID、统一错误响应、健康检查。
package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"cloud-drive/common/ctxdata"
)

type requestIDKey struct{}

// RequestID 是 go-zero rest 中间件：解析或生成 X-Request-Id，写入上下文与响应头。
// 有了它，日志、错误响应和前端反馈才能对上同一次请求。
func RequestID(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		next(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	}
}

// RequestIDFromCtx 返回中间件写入的请求 ID。
func RequestIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// WithRequestID 供测试或内部调用显式注入请求 ID。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "req-unknown"
	}
	return hex.EncodeToString(b)
}

// UserIDFromCtx 是 ctxdata.UserIdFromCtx 的别名，避免 handler 层到处引两个包。
func UserIDFromCtx(ctx context.Context) (uint64, bool) {
	return ctxdata.UserIdFromCtx(ctx)
}
