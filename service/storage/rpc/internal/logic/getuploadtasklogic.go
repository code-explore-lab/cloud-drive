package logic

import (
	"context"
	"errors"
	"time"

	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetUploadTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUploadTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUploadTaskLogic {
	return &GetUploadTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetUploadTask 查询上传进度。客户端据此决定哪些分片还需要重传（断点续传）。
func (l *GetUploadTaskLogic) GetUploadTask(in *storage.GetUploadTaskRequest) (*storage.GetUploadTaskResponse, error) {
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

	parts, err := l.svcCtx.PartsModel.FindByUploadId(l.ctx, task.UploadId)
	if err != nil {
		l.Errorf("find upload parts failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	items := make([]*storage.UploadPartInfo, 0, len(parts))
	for _, p := range parts {
		items = append(items, &storage.UploadPartInfo{
			PartNumber:  int32(p.PartNumber),
			Size:        int64(p.Size),
			Etag:        p.Etag,
			ContentHash: p.ContentHash,
		})
	}

	resp := &storage.GetUploadTaskResponse{
		UploadId:       task.UploadId,
		Status:         effectiveStatus(task, time.Now()),
		TotalSize:      int64(task.TotalSize),
		ChunkSize:      int64(task.ChunkSize),
		TotalChunks:    int64(task.TotalChunks),
		UploadedChunks: int64(len(parts)),
		Parts:          items,
		FileId:         task.FileId.Int64,
		FileName:       task.FileName,
		ContentType:    task.ContentType,
		ExpiresAt:      task.ExpiresAt.Unix(),
		ObjectKey:      task.ObjectKey,
	}
	for _, p := range parts {
		resp.UploadedBytes += int64(p.Size)
	}
	return resp, nil
}

// effectiveStatus 对已超时但还没被清理任务改写的任务返回 expired，
// 避免客户端以为还能继续传。
func effectiveStatus(task *model.UploadTasks, now time.Time) string {
	if (task.Status == "initiated" || task.Status == "uploading") && now.After(task.ExpiresAt) {
		return "expired"
	}
	return task.Status
}
