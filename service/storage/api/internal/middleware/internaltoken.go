package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// InternalTokenHeader 是服务间调用携带的共享令牌头。
const InternalTokenHeader = "X-Internal-Token"

// InternalTokenMiddleware 保护 /internal 接口：
// 只允许持有配置中共享密钥的内部服务访问，外部客户端即使猜到 URL 也拿不到对象。
type InternalTokenMiddleware struct {
	token string
}

func NewInternalTokenMiddleware(token string) *InternalTokenMiddleware {
	return &InternalTokenMiddleware{token: token}
}

func (m *InternalTokenMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.token == "" {
			// 没有配置密钥时直接拒绝，避免"忘记配置 = 全开放"。
			httpx.ErrorCtx(r.Context(), w, status.Error(codes.Unauthenticated, "internal token not configured"))
			return
		}
		got := r.Header.Get(InternalTokenHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(m.token)) != 1 {
			httpx.ErrorCtx(r.Context(), w, status.Error(codes.Unauthenticated, "invalid internal token"))
			return
		}
		next(w, r)
	}
}
