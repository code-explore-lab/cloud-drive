package logic

import (
	"context"
	"errors"

	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AbortUploadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAbortUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AbortUploadLogic {
	return &AbortUploadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// AbortUpload 取消上传任务：标记状态、删除已上传的临时分片。
// 幂等：重复取消返回成功。
func (l *AbortUploadLogic) AbortUpload(in *storage.AbortUploadRequest) (*storage.AbortUploadResponse, error) {
	if in.UploadId == "" || in.UserId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	task, err := l.svcCtx.TasksModel.FindOneByUploadId(l.ctx, in.UploadId)
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "upload task not found")
	}
	if err != nil {
		l.Errorf("find upload task failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if task.UserId != uint64(in.UserId) {
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}

	switch task.Status {
	case "aborted":
		return &storage.AbortUploadResponse{Success: true}, nil
	case "completed":
		return nil, status.Error(codes.FailedPrecondition, "upload already completed")
	}

	l.cleanupParts(task.UploadId)

	task.Status = "aborted"
	if err := l.svcCtx.TasksModel.Update(l.ctx, task); err != nil {
		l.Errorf("update upload task failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	l.Infof("upload aborted: upload_id=%s user_id=%d", task.UploadId, task.UserId)
	return &storage.AbortUploadResponse{Success: true}, nil
}

// cleanupParts 删除临时分片对象与分片记录。对象删除失败不影响任务取消，
// 残留对象由后台任务按 parts/ 前缀清理。
func (l *AbortUploadLogic) cleanupParts(uploadId string) {
	parts, err := l.svcCtx.PartsModel.FindByUploadId(l.ctx, uploadId)
	if err != nil {
		l.Errorf("find upload parts for cleanup failed: %v", err)
		return
	}
	for _, p := range parts {
		if p.ObjectPartId == "" {
			continue
		}
		if err := l.svcCtx.Store.Remove(l.ctx, p.ObjectPartId); err != nil {
			l.Errorf("remove temp part object %s failed: %v", p.ObjectPartId, err)
		}
	}
	if err := l.svcCtx.PartsModel.DeleteByUploadId(l.ctx, uploadId); err != nil {
		l.Errorf("delete upload parts failed: %v", err)
	}
}
