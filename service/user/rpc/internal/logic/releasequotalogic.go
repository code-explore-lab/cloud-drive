package logic

import (
	"context"

	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReleaseQuotaLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReleaseQuotaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReleaseQuotaLogic {
	return &ReleaseQuotaLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ReleaseQuotaLogic) ReleaseQuota(in *user.ReleaseQuotaRequest) (*user.ReleaseQuotaResponse, error) {
	if in.UserId <= 0 || in.ReleaseBytes <= 0 {
		return &user.ReleaseQuotaResponse{Success: false, Reason: "invalid argument"}, nil
	}

	l.Infof("release quota: user_id=%d bytes=%d request_id=%s", in.UserId, in.ReleaseBytes, in.RequestId)

	ok, err := l.svcCtx.QuotasModel.ReleaseQuota(l.ctx, uint64(in.UserId), uint64(in.ReleaseBytes))
	if err != nil {
		l.Errorf("release quota failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !ok {
		return &user.ReleaseQuotaResponse{Success: false, Reason: "release exceeds reserved quota"}, nil
	}

	return &user.ReleaseQuotaResponse{Success: true}, nil
}
