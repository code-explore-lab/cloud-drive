package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"
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

// AbortUpload 取消上传：先查进度拿到大小（用于归还预占额度），
// 再取消任务、归还额度、把文件标记为失败。重复取消不会重复归还。
func (l *AbortUploadLogic) AbortUpload(req *types.AbortUploadReq) (resp *types.AbortUploadResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.UploadID == "" {
		return nil, status.Error(codes.InvalidArgument, "uploadId is required")
	}

	task, err := l.svcCtx.StorageRpc.GetUploadTask(l.ctx, &storageclient.GetUploadTaskRequest{
		UploadId: req.UploadID,
		UserId:   int64(userId),
	})
	if err != nil {
		return nil, err
	}

	if _, err := l.svcCtx.StorageRpc.AbortUpload(l.ctx, &storageclient.AbortUploadRequest{
		UploadId:  req.UploadID,
		UserId:    int64(userId),
		RequestId: req.UploadID,
	}); err != nil {
		return nil, err
	}

	// 只有原本还在进行中的任务才需要归还预占额度。
	if task.Status == "initiated" || task.Status == "uploading" {
		releaseReserved(l.ctx, l.svcCtx, int64(userId), task.TotalSize, req.UploadID)
		if task.FileId > 0 {
			if _, err := l.svcCtx.FileRpc.UpdateFileStatus(l.ctx, &fileclient.UpdateFileStatusRequest{
				FileId:  task.FileId,
				OwnerId: int64(userId),
				Status:  "failed",
			}); err != nil {
				l.Errorf("mark file %d failed after abort: %v", task.FileId, err)
			}
		}
	}

	l.Infof("upload aborted: upload_id=%s user_id=%d", req.UploadID, userId)
	return &types.AbortUploadResp{Success: true}, nil
}
