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

type GetQuotaLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetQuotaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetQuotaLogic {
	return &GetQuotaLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetQuotaLogic) GetQuota() (resp *types.QuotaResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	rpcResp, err := l.svcCtx.UserRpc.GetQuota(l.ctx, &userclient.GetQuotaRequest{
		UserId: int64(userId),
	})
	if err != nil {
		return nil, err
	}

	return &types.QuotaResp{
		TotalBytes:    rpcResp.TotalBytes,
		UsedBytes:     rpcResp.UsedBytes,
		ReservedBytes: rpcResp.ReservedBytes,
	}, nil
}
