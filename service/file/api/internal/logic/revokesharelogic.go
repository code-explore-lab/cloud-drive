package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RevokeShareLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRevokeShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RevokeShareLogic {
	return &RevokeShareLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RevokeShareLogic) RevokeShare(req *types.ShareIDReq) (resp *types.RevokeShareResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.ShareID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid share id")
	}

	rpcResp, err := l.svcCtx.FileRpc.RevokeShare(l.ctx, &fileclient.RevokeShareRequest{
		ShareId: req.ShareID,
		OwnerId: int64(userId),
	})
	if err != nil {
		return nil, err
	}

	return &types.RevokeShareResp{Success: rpcResp.Success}, nil
}
