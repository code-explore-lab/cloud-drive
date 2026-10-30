package logic

import (
	"context"

	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReserveQuotaLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReserveQuotaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReserveQuotaLogic {
	return &ReserveQuotaLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ReserveQuotaLogic) ReserveQuota(in *user.ReserveQuotaRequest) (*user.ReserveQuotaResponse, error) {
	if in.UserId <= 0 || in.RequestBytes <= 0 {
		return &user.ReserveQuotaResponse{Success: false, Reason: "invalid argument"}, nil
	}

	// request_id 目前只用于日志追踪；真正的幂等需要单独的去重表，后续再补
	l.Infof("reserve quota: user_id=%d bytes=%d request_id=%s", in.UserId, in.RequestBytes, in.RequestId)

	ok, err := l.svcCtx.QuotasModel.ReserveQuota(l.ctx, uint64(in.UserId), uint64(in.RequestBytes))
	if err != nil {
		l.Errorf("reserve quota failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !ok {
		return &user.ReserveQuotaResponse{Success: false, Reason: "insufficient quota"}, nil
	}

	// 返回最新的 reserved_bytes，让调用方知道当前占用
	q, err := l.svcCtx.QuotasModel.FindOneByUserId(l.ctx, uint64(in.UserId))
	if err != nil {
		l.Errorf("find quota failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &user.ReserveQuotaResponse{
		Success:       true,
		ReservedBytes: int64(q.ReservedBytes),
	}, nil
}
