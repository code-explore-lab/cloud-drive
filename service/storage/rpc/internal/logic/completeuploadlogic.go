package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud-drive/common/metrics"
	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CompleteUploadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCompleteUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CompleteUploadLogic {
	return &CompleteUploadLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CompleteUpload 把分片合并成最终对象，并落 storage_objects 元数据。
//
// 一致性说明（详见 docs/consistency.md）：
//   - 合并对象是不可回滚的外部操作，所以先把任务推进到 completing，再做合并；
//   - 合并成功后写对象元数据、最后把任务置为 completed；
//   - 中途失败把任务置为 failed，重复调用会重新走一遍（合并是幂等的覆盖写）。
func (l *CompleteUploadLogic) CompleteUpload(in *storage.CompleteUploadRequest) (*storage.CompleteUploadResponse, error) {
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
	case "completed":
		// 重复完成请求：直接返回已有结果，保证幂等。
		return uploadTaskToCompleteResponse(task, false), nil
	case "completing":
		return nil, status.Error(codes.FailedPrecondition, "upload is being completed, retry later")
	case "aborted":
		return nil, status.Error(codes.FailedPrecondition, "upload task was aborted")
	case "failed":
		return nil, status.Error(codes.FailedPrecondition, "upload task failed")
	}
	if time.Now().After(task.ExpiresAt) {
		l.markFailed(task, "upload task expired")
		return nil, status.Error(codes.FailedPrecondition, "upload task expired")
	}

	parts, err := l.svcCtx.PartsModel.FindByUploadId(l.ctx, task.UploadId)
	if err != nil {
		l.Errorf("find upload parts failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	claimed, err := l.svcCtx.TasksModel.ClaimForComplete(l.ctx, task)
	if err != nil {
		l.Errorf("claim upload task failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !claimed {
		// 另一个请求已经抢到合并权，重新读取状态决定返回什么。
		latest, findErr := l.svcCtx.TasksModel.FindOneByUploadId(l.ctx, task.UploadId)
		if findErr == nil && latest.Status == "completed" {
			return uploadTaskToCompleteResponse(latest, false), nil
		}
		return nil, status.Error(codes.FailedPrecondition, "upload is being completed, retry later")
	}

	if err := validateParts(task, parts); err != nil {
		l.markFailed(task, err.Error())
		return nil, status.Errorf(codes.FailedPrecondition, "cannot complete upload: %v", err)
	}

	objectKey := task.ObjectKey
	if objectKey == "" {
		objectKey = objectKeyFor(task.UserId, task.UploadId, task.FileName)
		task.ObjectKey = objectKey
	}

	partKeys := make([]string, 0, len(parts))
	for _, p := range parts {
		partKeys = append(partKeys, p.ObjectPartId)
	}

	if len(partKeys) == 1 {
		// 单片上传不需要合并，服务端复制一次即可。
		err = l.svcCtx.Store.Copy(l.ctx, objectKey, partKeys[0], task.ContentType)
	} else {
		err = l.svcCtx.Store.Compose(l.ctx, objectKey, partKeys)
	}
	if err != nil {
		l.markFailed(task, fmt.Sprintf("merge parts failed: %v", err))
		return nil, status.Error(codes.Internal, "merge parts failed")
	}

	info, err := l.svcCtx.Store.Stat(l.ctx, objectKey)
	if err != nil {
		_ = l.svcCtx.Store.Remove(l.ctx, objectKey)
		l.markFailed(task, fmt.Sprintf("stat merged object failed: %v", err))
		return nil, status.Error(codes.Internal, "verify merged object failed")
	}
	if uint64(info.Size) != task.TotalSize {
		_ = l.svcCtx.Store.Remove(l.ctx, objectKey)
		l.markFailed(task, fmt.Sprintf("merged object size mismatch: got %d want %d",
			info.Size, task.TotalSize))
		return nil, status.Error(codes.Internal, "merged object size mismatch")
	}

	// 客户端提供了摘要时才做整文件校验：这需要完整读一遍对象，代价是真实存在的，
	// 因此不作为默认行为（见 ADR-0005）。
	hashVerified := false
	if task.ExpectedHash != "" {
		if err := l.svcCtx.Store.VerifySHA256(l.ctx, objectKey, task.ExpectedHash); err != nil {
			_ = l.svcCtx.Store.Remove(l.ctx, objectKey)
			l.markFailed(task, fmt.Sprintf("content hash mismatch: %v", err))
			return nil, status.Error(codes.InvalidArgument, "content hash mismatch")
		}
		hashVerified = true
	}

	metrics.UploadBytesTotal.Add(float64(task.TotalSize))
	metrics.UploadCompletedTotal.WithLabelValues("success").Inc()

	if err := l.upsertObject(task, objectKey, hashVerified); err != nil {
		// 对象已经合并成功但元数据没写上，标记失败以便重试；重试会覆盖同一对象。
		l.markFailed(task, fmt.Sprintf("upsert storage object failed: %v", err))
		return nil, status.Error(codes.Internal, "record storage object failed")
	}

	// 合并完成后临时分片就没用了。删除失败只记录日志，由后台任务按前缀清理。
	for _, key := range partKeys {
		if err := l.svcCtx.Store.Remove(l.ctx, key); err != nil {
			l.Errorf("remove temp part object %s failed: %v", key, err)
		}
	}

	task.Status = "completed"
	task.UploadedChunks = task.TotalChunks
	task.UploadedBytes = task.TotalSize
	task.LastError = ""
	if err := l.svcCtx.TasksModel.Update(l.ctx, task); err != nil {
		l.Errorf("update upload task failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	l.Infof("upload completed: upload_id=%s user_id=%d object_key=%s size=%d verified=%v",
		task.UploadId, task.UserId, objectKey, task.TotalSize, hashVerified)

	return uploadTaskToCompleteResponse(task, hashVerified), nil
}

// upsertObject 让 storage_objects 与对象存储保持一致：已存在则更新，不存在则插入。
func (l *CompleteUploadLogic) upsertObject(task *model.UploadTasks, objectKey string, verified bool) error {
	obj, err := l.svcCtx.ObjectsModel.FindOneByObjectKey(l.ctx, objectKey)
	switch {
	case err == nil:
		obj.Size = task.TotalSize
		obj.ContentType = task.ContentType
		obj.ContentHash = task.ExpectedHash
		obj.HashVerified = boolToInt(verified)
		obj.Status = "available"
		return l.svcCtx.ObjectsModel.Update(l.ctx, obj)
	case !errors.Is(err, model.ErrNotFound):
		return err
	}

	_, err = l.svcCtx.ObjectsModel.Insert(l.ctx, &model.StorageObjects{
		ObjectKey:    objectKey,
		Backend:      "minio",
		BucketName:   l.svcCtx.Store.Bucket(),
		Size:         task.TotalSize,
		ContentType:  task.ContentType,
		ContentHash:  task.ExpectedHash,
		HashVerified: boolToInt(verified),
		Status:       "available",
	})
	return err
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (l *CompleteUploadLogic) markFailed(task *model.UploadTasks, reason string) {
	metrics.UploadCompletedTotal.WithLabelValues("failed").Inc()
	l.Errorf("upload %s failed: %s", task.UploadId, reason)
	task.Status = "failed"
	task.LastError = truncate(reason, 500)
	if err := l.svcCtx.TasksModel.Update(l.ctx, task); err != nil {
		l.Errorf("mark upload task failed: %v", err)
	}
}

func uploadTaskToCompleteResponse(task *model.UploadTasks, verified bool) *storage.CompleteUploadResponse {
	return &storage.CompleteUploadResponse{
		ObjectKey:    task.ObjectKey,
		Size:         int64(task.TotalSize),
		ContentHash:  task.ExpectedHash,
		Success:      true,
		FileId:       task.FileId.Int64,
		HashVerified: verified,
	}
}
