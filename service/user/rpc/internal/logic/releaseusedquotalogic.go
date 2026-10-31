package logic

import (
	"context"

	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReleaseUsedQuotaLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReleaseUsedQuotaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReleaseUsedQuotaLogic {
	return &ReleaseUsedQuotaLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ReleaseUsedQuota 在文件删除后归还已用额度。
func (l *ReleaseUsedQuotaLogic) ReleaseUsedQuota(in *user.ReleaseUsedQuotaRequest) (*user.ReleaseUsedQuotaResponse, error) {
	if in.UserId <= 0 || in.ReleaseBytes < 0 {
		return &user.ReleaseUsedQuotaResponse{Success: false, Reason: "invalid argument"}, nil
	}
	if in.ReleaseBytes == 0 {
		return &user.ReleaseUsedQuotaResponse{Success: true}, nil
	}

	ok, err := l.svcCtx.QuotasModel.ReleaseUsedQuota(l.ctx, uint64(in.UserId), uint64(in.ReleaseBytes))
	if err != nil {
		l.Errorf("release used quota failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !ok {
		// 归还量超过已用量：说明账目已经不一致，记录错误交给对账处理，不把值改成负数。
		l.Errorf("release used quota failed: used_bytes < %d, user_id=%d request_id=%s",
			in.ReleaseBytes, in.UserId, in.RequestId)
		return &user.ReleaseUsedQuotaResponse{Success: false, Reason: "release exceeds used quota"}, nil
	}

	l.Infof("used quota released: user_id=%d bytes=%d request_id=%s",
		in.UserId, in.ReleaseBytes, in.RequestId)
	return &user.ReleaseUsedQuotaResponse{Success: true}, nil
}
