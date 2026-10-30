// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/user/api/internal/svc"
	"cloud-drive/service/user/api/internal/types"
	"cloud-drive/service/user/rpc/userclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetUserInfoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserInfoLogic {
	return &GetUserInfoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserInfoLogic) GetUserInfo() (resp *types.UserInfoResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	rpcResp, err := l.svcCtx.UserRpc.GetUser(l.ctx, &userclient.GetUserRequest{
		UserId: int64(userId),
	})
	if err != nil {
		return nil, err
	}

	return &types.UserInfoResp{
		UserID:    rpcResp.UserId,
		Username:  rpcResp.Username,
		Email:     rpcResp.Email,
		CreatedAt: rpcResp.CreatedAt,
	}, nil
}
