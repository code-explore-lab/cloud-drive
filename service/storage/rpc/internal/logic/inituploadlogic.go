package logic

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InitUploadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewInitUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *InitUploadLogic {
	return &InitUploadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// InitUpload 创建上传会话，返回分片大小和总分片数。
// 幂等：同一个 upload_id 重复调用返回已有任务，不会重复建单。
func (l *InitUploadLogic) InitUpload(in *storage.InitUploadRequest) (*storage.InitUploadResponse, error) {
	if in.UploadId == "" || in.UserId <= 0 || in.FileName == "" || in.Size < 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	task, err := l.svcCtx.TasksModel.FindOneByUploadId(l.ctx, in.UploadId)
	switch {
	case err == nil:
		if task.UserId != uint64(in.UserId) {
			return nil, status.Error(codes.PermissionDenied, "permission denied")
		}
		return uploadTaskToInitResponse(task), nil
	case !errors.Is(err, model.ErrNotFound):
		l.Errorf("find upload task failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	contentType := in.ContentType
	if contentType == "" {
		contentType = defaultContentType
	}

	chunkSize := int64(defaultChunkSize)
	totalChunks := (in.Size + chunkSize - 1) / chunkSize
	if totalChunks == 0 {
		// 空文件也占一个分片位置，避免 0 分片合并的边界问题。
		totalChunks = 1
	}
	if totalChunks > maxTotalChunks {
		return nil, status.Errorf(codes.InvalidArgument,
			"file too large: %d chunks exceeds limit %d", totalChunks, maxTotalChunks)
	}

	now := time.Now()
	newTask := &model.UploadTasks{
		UploadId:     in.UploadId,
		UserId:       uint64(in.UserId),
		FileId:       sql.NullInt64{Int64: in.FileId, Valid: in.FileId > 0},
		FileName:     in.FileName,
		TotalSize:    uint64(in.Size),
		ContentType:  contentType,
		ExpectedHash: in.ContentHash,
		ObjectKey:    objectKeyFor(uint64(in.UserId), in.UploadId, in.FileName),
		ChunkSize:    uint64(chunkSize),
		TotalChunks:  uint64(totalChunks),
		Status:       "initiated",
		ExpiresAt:    now.Add(uploadTTL),
	}
	if _, err := l.svcCtx.TasksModel.Insert(l.ctx, newTask); err != nil {
		// 并发重试可能撞 upload_id 唯一键，回查一次再决定是否成功。
		if existing, findErr := l.svcCtx.TasksModel.FindOneByUploadId(l.ctx, in.UploadId); findErr == nil {
			if existing.UserId != uint64(in.UserId) {
				return nil, status.Error(codes.PermissionDenied, "permission denied")
			}
			return uploadTaskToInitResponse(existing), nil
		}
		l.Errorf("insert upload task failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	l.Infof("upload task created: upload_id=%s user_id=%d size=%d chunks=%d",
		newTask.UploadId, newTask.UserId, newTask.TotalSize, newTask.TotalChunks)

	return uploadTaskToInitResponse(newTask), nil
}

func uploadTaskToInitResponse(task *model.UploadTasks) *storage.InitUploadResponse {
	return &storage.InitUploadResponse{
		UploadId:    task.UploadId,
		ChunkSize:   int64(task.ChunkSize),
		TotalChunks: int64(task.TotalChunks),
		Success:     true,
		ObjectKey:   task.ObjectKey,
		ExpiresAt:   task.ExpiresAt.Unix(),
	}
}
