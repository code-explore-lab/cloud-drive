package logic

import (
	"context"

	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetQuotaLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetQuotaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetQuotaLogic {
	return &GetQuotaLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetQuotaLogic) GetQuota(in *user.GetQuotaRequest) (*user.GetQuotaResponse, error) {
	q, err := l.svcCtx.QuotasModel.FindOneByUserId(l.ctx, uint64(in.UserId))
	if err != nil {
		return nil, err
	}
	return &user.GetQuotaResponse{
		TotalBytes:    int64(q.TotalBytes),
		UsedBytes:     int64(q.UsedBytes),
		ReservedBytes: int64(q.ReservedBytes),
	}, nil
}
