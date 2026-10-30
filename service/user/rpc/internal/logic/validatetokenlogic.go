package logic

import (
	"context"
	"errors"

	"cloud-drive/common/jwtx"
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ValidateTokenLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewValidateTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ValidateTokenLogic {
	return &ValidateTokenLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ValidateTokenLogic) ValidateToken(in *user.ValidateTokenRequest) (*user.ValidateTokenResponse, error) {
	if in.AccessToken == "" {
		return &user.ValidateTokenResponse{Valid: false}, nil
	}

	claims, err := jwtx.Parse(l.svcCtx.Config.JwtSecret, in.AccessToken)
	if err != nil {
		return &user.ValidateTokenResponse{Valid: false}, nil
	}

	// JWT 只在签发时可信，用户可能已被禁用或删除，所以还要查一次库
	u, err := l.svcCtx.UsersModel.FindOne(l.ctx, claims.UserId)
	if errors.Is(err, model.ErrNotFound) {
		return &user.ValidateTokenResponse{Valid: false}, nil
	}
	if err != nil {
		l.Errorf("find user failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if u.Status != 1 {
		return &user.ValidateTokenResponse{Valid: false}, nil
	}

	return &user.ValidateTokenResponse{
		Valid:    true,
		UserId:   int64(u.Id),
		Username: u.Username,
	}, nil
}
