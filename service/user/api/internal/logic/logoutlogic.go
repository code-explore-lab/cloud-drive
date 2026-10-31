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

type LogoutLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLogoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LogoutLogic {
	return &LogoutLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Logout 撤销 refresh token。不传 refreshToken 时撤销该用户全部会话。
func (l *LogoutLogic) Logout(req *types.LogoutReq) (resp *types.LogoutResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	rpcResp, err := l.svcCtx.UserRpc.Logout(l.ctx, &userclient.LogoutRequest{
		UserId:       int64(userId),
		RefreshToken: req.RefreshToken,
	})
	if err != nil {
		return nil, err
	}

	return &types.LogoutResp{Success: rpcResp.Success, RevokedCount: rpcResp.RevokedCount}, nil
}
