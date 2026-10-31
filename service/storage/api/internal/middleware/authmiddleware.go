package middleware

import (
	"net/http"
	"strings"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/storage/api/internal/svc"
	"cloud-drive/service/user/rpc/userclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const bearerPrefix = "Bearer "

// AuthMiddleware 通过 user-rpc 校验 Access Token，并把 userId 注入上下文。
// 数据面接口（分片上传）必须复用同一套鉴权，不能因为"是内部服务"就放行。
type AuthMiddleware struct {
	svcCtx *svc.ServiceContext
}

func NewAuthMiddleware(svcCtx *svc.ServiceContext) *AuthMiddleware {
	return &AuthMiddleware{svcCtx: svcCtx}
}

func (m *AuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			httpx.ErrorCtx(r.Context(), w, status.Error(codes.Unauthenticated, "missing bearer token"))
			return
		}

		resp, err := m.svcCtx.UserRpc.ValidateToken(r.Context(), &userclient.ValidateTokenRequest{
			AccessToken: token,
		})
		if err != nil {
			logx.WithContext(r.Context()).Errorf("validate token failed: %v", err)
			code := status.Code(err)
			if code != codes.Unavailable && code != codes.DeadlineExceeded {
				code = codes.Internal
			}
			httpx.ErrorCtx(r.Context(), w, status.Error(code, "failed to validate token"))
			return
		}
		if !resp.Valid {
			httpx.ErrorCtx(r.Context(), w, status.Error(codes.Unauthenticated, "invalid or expired token"))
			return
		}

		ctx := ctxdata.WithUserId(r.Context(), uint64(resp.UserId))
		next(w, r.WithContext(ctx))
	}
}

func bearerToken(header string) (string, bool) {
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(bearerPrefix):])
	return token, token != ""
}
