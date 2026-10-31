// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/storage/api/internal/svc"
	"cloud-drive/service/storage/api/internal/types"
	"cloud-drive/service/storage/rpc/storageclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AbortUploadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAbortUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AbortUploadLogic {
	return &AbortUploadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AbortUpload 取消上传并清理临时分片。重复取消是幂等的。
func (l *AbortUploadLogic) AbortUpload(req *types.AbortUploadReq) (resp *types.AbortUploadResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	if req.UploadID == "" {
		return nil, status.Error(codes.InvalidArgument, "uploadId is required")
	}

	rpcResp, err := l.svcCtx.StorageRpc.AbortUpload(l.ctx, &storageclient.AbortUploadRequest{
		UploadId:  req.UploadID,
		UserId:    int64(userId),
		RequestId: req.UploadID,
	})
	if err != nil {
		return nil, err
	}
	if !rpcResp.Success {
		return nil, status.Error(codes.Internal, "abort upload failed")
	}

	return &types.AbortUploadResp{Success: true}, nil
}
