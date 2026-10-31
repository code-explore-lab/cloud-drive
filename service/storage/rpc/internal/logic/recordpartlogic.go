package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RecordPartLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRecordPartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecordPartLogic {
	return &RecordPartLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RecordPart 记录一个已经成功写入对象存储的分片。
// 提交顺序是"先写对象、后写元数据"，因此这里失败时对象已经是可用的临时分片，
// 最多留下孤儿临时对象，不会产生"元数据说成功、对象其实不存在"的坏状态。
func (l *RecordPartLogic) RecordPart(in *storage.RecordPartRequest) (*storage.RecordPartResponse, error) {
	if in.UploadId == "" || in.UserId <= 0 || in.PartNumber <= 0 || in.Size < 0 {
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
	if task.Status != "initiated" && task.Status != "uploading" {
		return nil, status.Errorf(codes.FailedPrecondition, "upload task is %s", task.Status)
	}
	if time.Now().After(task.ExpiresAt) {
		return nil, status.Error(codes.FailedPrecondition, "upload task expired")
	}
	if in.PartNumber > int32(task.TotalChunks) {
		return nil, status.Errorf(codes.OutOfRange,
			"part number %d exceeds total chunks %d", in.PartNumber, task.TotalChunks)
	}

	want := expectedPartSize(task, in.PartNumber)
	if in.Size != want {
		return nil, status.Errorf(codes.InvalidArgument,
			"part %d size mismatch: got %d want %d", in.PartNumber, in.Size, want)
	}

	// 只允许写服务端约定好的临时 Key，避免调用方指定任意对象路径。
	expectedKey := partObjectKey(task.UploadId, in.PartNumber)
	if in.ObjectPartId != "" && in.ObjectPartId != expectedKey {
		return nil, status.Error(codes.InvalidArgument, "invalid object part key")
	}

	if err := l.svcCtx.PartsModel.UpsertPart(l.ctx, &model.UploadParts{
		UploadId:     task.UploadId,
		PartNumber:   uint64(in.PartNumber),
		Size:         uint64(in.Size),
		ContentHash:  in.ContentHash,
		Etag:         in.Etag,
		ObjectPartId: expectedKey,
	}); err != nil {
		l.Errorf("upsert upload part failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	parts, err := l.svcCtx.PartsModel.FindByUploadId(l.ctx, task.UploadId)
	if err != nil {
		l.Errorf("find upload parts failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	var uploadedBytes uint64
	for _, p := range parts {
		uploadedBytes += p.Size
	}

	task.UploadedChunks = uint64(len(parts))
	task.UploadedBytes = uploadedBytes
	if task.Status == "initiated" {
		task.Status = "uploading"
	}
	if err := l.svcCtx.TasksModel.Update(l.ctx, task); err != nil {
		l.Errorf("update upload task progress failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	l.Infof("upload part recorded: upload_id=%s part=%d size=%d progress=%d/%d",
		task.UploadId, in.PartNumber, in.Size, len(parts), task.TotalChunks)

	return &storage.RecordPartResponse{
		Success:        true,
		UploadedChunks: int64(len(parts)),
		UploadedBytes:  int64(uploadedBytes),
	}, nil
}

// validateParts 校验分片是否齐全、序号连续、大小符合预期。
// 返回的错误信息会写进 last_error，便于排查。
func validateParts(task *model.UploadTasks, parts []*model.UploadParts) error {
	if len(parts) != int(task.TotalChunks) {
		return fmt.Errorf("upload incomplete: %d/%d parts uploaded", len(parts), task.TotalChunks)
	}
	var total uint64
	for i, p := range parts {
		wantNumber := int32(i + 1)
		if int32(p.PartNumber) != wantNumber {
			return fmt.Errorf("missing part %d", wantNumber)
		}
		if p.ObjectPartId == "" {
			return fmt.Errorf("part %d has no object", wantNumber)
		}
		if want := expectedPartSize(task, int32(p.PartNumber)); int64(p.Size) != want {
			return fmt.Errorf("part %d size mismatch: got %d want %d", p.PartNumber, p.Size, want)
		}
		total += p.Size
	}
	if total != task.TotalSize {
		return fmt.Errorf("total size mismatch: got %d want %d", total, task.TotalSize)
	}
	return nil
}
