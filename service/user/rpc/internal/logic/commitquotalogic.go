package logic

import (
	"context"

	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CommitQuotaLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCommitQuotaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CommitQuotaLogic {
	return &CommitQuotaLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CommitQuota 把上传时预占的额度转成实际占用。
// 幂等由调用方保证（文件状态只迁移一次），这里只做原子账务处理。
func (l *CommitQuotaLogic) CommitQuota(in *user.CommitQuotaRequest) (*user.CommitQuotaResponse, error) {
	if in.UserId <= 0 || in.Bytes < 0 {
		return &user.CommitQuotaResponse{Success: false, Reason: "invalid argument"}, nil
	}
	if in.Bytes == 0 {
		return &user.CommitQuotaResponse{Success: true}, nil
	}

	ok, err := l.svcCtx.QuotasModel.CommitQuota(l.ctx, uint64(in.UserId), uint64(in.Bytes))
	if err != nil {
		l.Errorf("commit quota failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !ok {
		// 预占不足通常意味着重复提交或账目漂移，返回失败让上层记录异常而不是静默吞掉。
		l.Errorf("commit quota failed: reserved quota is less than %d, user_id=%d request_id=%s",
			in.Bytes, in.UserId, in.RequestId)
		return &user.CommitQuotaResponse{Success: false, Reason: "insufficient reserved quota"}, nil
	}

	l.Infof("quota committed: user_id=%d bytes=%d request_id=%s", in.UserId, in.Bytes, in.RequestId)
	return &user.CommitQuotaResponse{Success: true}, nil
}
