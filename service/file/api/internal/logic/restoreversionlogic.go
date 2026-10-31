package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RestoreVersionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRestoreVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RestoreVersionLogic {
	return &RestoreVersionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// RestoreVersion 把文件回滚到指定版本。回滚本身不改变已用配额
// （对象一直存在，只是 files 指针换了目标），所以这里不需要动额度。
func (l *RestoreVersionLogic) RestoreVersion(req *types.RestoreVersionReq) (resp *types.RestoreVersionResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.FileID <= 0 || req.Version <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	rpcResp, err := l.svcCtx.FileRpc.RestoreVersion(l.ctx, &fileclient.RestoreVersionRequest{
		FileId:    req.FileID,
		UserId:    int64(userId),
		Version:   req.Version,
		RequestId: uuid.NewString(),
	})
	if err != nil {
		return nil, err
	}

	return &types.RestoreVersionResp{Success: rpcResp.Success, Version: rpcResp.Version}, nil
}
