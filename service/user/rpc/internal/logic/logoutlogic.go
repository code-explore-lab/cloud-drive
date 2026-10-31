package logic

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LogoutLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLogoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LogoutLogic {
	return &LogoutLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Logout 撤销 refresh token：
//   - 传了 refresh_token 就只撤销这一个会话（单设备退出）；
//   - 没传就撤销该用户全部有效会话（退出所有设备）。
//
// access token 是无状态 JWT，无法逐个撤销；它的有效期只有 15 分钟，
// 这是"愿意用短期 token 换取无状态鉴权"的显式取舍（见 docs/security.md）。
func (l *LogoutLogic) Logout(in *user.LogoutRequest) (*user.LogoutResponse, error) {
	if in.UserId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}
	now := time.Now()

	if in.RefreshToken != "" {
		token, err := l.svcCtx.TokensModel.FindOneByTokenHash(l.ctx, hashToken(in.RefreshToken))
		if errors.Is(err, model.ErrNotFound) {
			// 幂等：token 本来就不存在，退出也算成功。
			return &user.LogoutResponse{Success: true}, nil
		}
		if err != nil {
			l.Errorf("find refresh token failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
		if token.UserId != uint64(in.UserId) || token.RevokedAt.Valid {
			return &user.LogoutResponse{Success: true}, nil
		}

		token.RevokedAt = sql.NullTime{Time: now, Valid: true}
		if err := l.svcCtx.TokensModel.Update(l.ctx, token); err != nil {
			l.Errorf("revoke refresh token failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
		return &user.LogoutResponse{Success: true, RevokedCount: 1}, nil
	}

	tokens, err := l.svcCtx.TokensModel.FindActiveByUser(l.ctx, uint64(in.UserId), now)
	if err != nil {
		l.Errorf("find active refresh tokens failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	var revoked int64
	for _, token := range tokens {
		token.RevokedAt = sql.NullTime{Time: now, Valid: true}
		if err := l.svcCtx.TokensModel.Update(l.ctx, token); err != nil {
			l.Errorf("revoke refresh token %d failed: %v", token.Id, err)
			continue
		}
		revoked++
	}

	l.Infof("user logged out: user_id=%d revoked=%d", in.UserId, revoked)
	return &user.LogoutResponse{Success: true, RevokedCount: revoked}, nil
}
