package middleware

import (
	"net"
	"net/http"
	"strings"

	"cloud-drive/common/ratelimit"

	"github.com/zeromicro/go-zero/rest/httpx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RateLimitMiddleware 对敏感接口做按客户端 IP 的限流。
// 超限返回 429（ResourceExhausted），而不是静默丢弃。
type RateLimitMiddleware struct {
	limiter *ratelimit.Limiter
}

func NewRateLimitMiddleware(rps float64, burst int) *RateLimitMiddleware {
	if burst <= 0 {
		burst = 10
	}
	return &RateLimitMiddleware{limiter: ratelimit.New(rps, float64(burst))}
}

func (m *RateLimitMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.limiter.Allow(clientIP(r)) {
			httpx.ErrorCtx(r.Context(), w, status.Error(codes.ResourceExhausted, "too many requests"))
			return
		}
		next(w, r)
	}
}

// clientIP 取客户端地址。反向代理场景下优先用 X-Forwarded-For 的第一段，
// 部署文档里要求只信任自己控制的代理设置该头（见 docs/security.md）。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
