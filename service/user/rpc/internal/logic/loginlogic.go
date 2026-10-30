package logic

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"cloud-drive/common/cryptx"
	"cloud-drive/common/jwtx"
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LoginLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *LoginLogic) Login(in *user.LoginRequest) (*user.LoginResponse, error) {
	u, err := l.svcCtx.UsersModel.FindOneByUsername(l.ctx, in.Username)
	// 用户不存在和密码错误返回同一句话，避免暴露用户名是否存在
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.Unauthenticated, "invalid username or password")
	}
	if err != nil {
		l.Errorf("find user by username failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if u.Status != 1 {
		return nil, status.Error(codes.PermissionDenied, "account disabled")
	}

	got := cryptx.PasswordEncrypt(l.svcCtx.Config.Salt, in.Password)
	if subtle.ConstantTimeCompare([]byte(got), []byte(u.PasswordHash)) != 1 {
		return nil, status.Error(codes.Unauthenticated, "invalid username or password")
	}

	now := time.Now()
	accessExp := now.Add(accessTokenTTL)

	accessToken, err := jwtx.Generate(l.svcCtx.Config.JwtSecret, u.Id, u.Username, accessExp)
	if err != nil {
		l.Errorf("issue access token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	refreshToken, err := randomToken()
	if err != nil {
		l.Errorf("generate refresh token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if _, err := l.svcCtx.TokensModel.Insert(l.ctx, &model.RefreshTokens{
		UserId:    u.Id,
		TokenHash: hashToken(refreshToken),
		ExpiresAt: now.Add(refreshTokenTTL),
	}); err != nil {
		l.Errorf("store refresh token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &user.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    accessExp.Unix(),
	}, nil
}
