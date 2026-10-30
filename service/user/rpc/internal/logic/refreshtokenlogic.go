package logic

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"cloud-drive/common/jwtx"
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RefreshTokenLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRefreshTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RefreshTokenLogic {
	return &RefreshTokenLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *RefreshTokenLogic) RefreshToken(in *user.RefreshTokenRequest) (*user.RefreshTokenResponse, error) {
	// 客户端给的是明文 token，库里存的是哈希，所以先哈希再查
	old, err := l.svcCtx.TokensModel.FindOneByTokenHash(l.ctx, hashToken(in.RefreshToken))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if err != nil {
		l.Errorf("find refresh token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	now := time.Now()
	if old.RevokedAt.Valid {
		l.Errorf("refresh token reused: id=%d user_id=%d", old.Id, old.UserId)
		return nil, status.Error(codes.Unauthenticated, "refresh token already revoked")
	}
	if now.After(old.ExpiresAt) {
		return nil, status.Error(codes.Unauthenticated, "refresh token expired")
	}

	u, err := l.svcCtx.UsersModel.FindOne(l.ctx, old.UserId)
	if err != nil {
		return nil, err
	}
	if u.Status != 1 {
		return nil, status.Error(codes.PermissionDenied, "account disabled")
	}

	accessExp := now.Add(accessTokenTTL)

	accessToken, err := jwtx.Generate(l.svcCtx.Config.JwtSecret, u.Id, u.Username, accessExp)
	if err != nil {
		l.Errorf("issue access token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	newRefresh, err := randomToken()
	if err != nil {
		l.Errorf("generate refresh token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	// 先存新的，再作废旧的。顺序反过来一旦中途失败会把用户强制登出。
	if _, err := l.svcCtx.TokensModel.Insert(l.ctx, &model.RefreshTokens{
		UserId:    u.Id,
		TokenHash: hashToken(newRefresh),
		ExpiresAt: now.Add(refreshTokenTTL),
	}); err != nil {
		l.Errorf("store refresh token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	old.RevokedAt = sql.NullTime{Time: now, Valid: true}
	if err := l.svcCtx.TokensModel.Update(l.ctx, old); err != nil {
		l.Errorf("revoke old refresh token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &user.RefreshTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
		ExpiresAt:    accessExp.Unix(),
	}, nil
}
