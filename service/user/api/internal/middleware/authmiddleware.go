package middleware

import (
	"net/http"
	"strings"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/user/api/internal/svc"
	"cloud-drive/service/user/rpc/userclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const bearerPrefix = "Bearer "

type AuthMiddleware struct {
	svcCtx *svc.ServiceContext
}

func NewAuthMiddleware(svcCtx *svc.ServiceContext) *AuthMiddleware {
	return &AuthMiddleware{svcCtx: svcCtx}
}

// Handle validates the access token through user-rpc and injects the user id
// into the request context for downstream logic.
func (m *AuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			httpx.ErrorCtx(r.Context(), w, status.Error(codes.Unauthenticated, "missing or invalid authorization header"))
			return
		}

		resp, err := m.svcCtx.UserRpc.ValidateToken(r.Context(), &userclient.ValidateTokenRequest{
			AccessToken: token,
		})
		if err != nil {
			logx.WithContext(r.Context()).Errorf("validate token failed: %v", err)
			code := status.Code(err)
			if code == codes.Unavailable || code == codes.DeadlineExceeded {
				httpx.ErrorCtx(r.Context(), w, status.Error(code, "user service unavailable"))
				return
			}
			httpx.ErrorCtx(r.Context(), w, status.Error(codes.Internal, "failed to validate token"))
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
