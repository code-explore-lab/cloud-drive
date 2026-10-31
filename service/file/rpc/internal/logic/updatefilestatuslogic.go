package logic

import (
	"context"
	"errors"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdateFileStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateFileStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateFileStatusLogic {
	return &UpdateFileStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func validFileStatus(s string) bool {
	switch s {
	case "pending", "uploading", "processing", "available", "failed", "deleting":
		return true
	}
	return false
}

// UpdateFileStatus 处理除"发布内容"以外的状态迁移。
//
// 为什么 available 不能走这里：文件内容与版本必须一起变（files 指针 + file_versions 新行），
// 那是一个事务，由 AddVersion 负责。这个接口只负责 failed/deleting 这类纯标记迁移。
// 返回值里的 changed 让编排层知道"这次调用是否真的改了状态"，从而避免重复扣配额。
func (l *UpdateFileStatusLogic) UpdateFileStatus(in *file.UpdateFileStatusRequest) (*file.UpdateFileStatusResponse, error) {
	if in.FileId <= 0 || in.OwnerId <= 0 || !validFileStatus(in.Status) {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}
	if in.Status == "available" {
		return nil, status.Error(codes.FailedPrecondition, "use AddVersion to publish file content")
	}

	f, err := l.svcCtx.FilesModel.FindOne(l.ctx, uint64(in.FileId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err != nil {
		l.Errorf("find file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if f.OwnerId != uint64(in.OwnerId) {
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
	if f.DeletedAt.Valid {
		return nil, status.Error(codes.NotFound, "file not found")
	}

	resp := &file.UpdateFileStatusResponse{Success: true, PreviousStatus: f.Status}
	if f.Status == in.Status {
		return resp, nil
	}

	f.Status = in.Status
	if err := l.svcCtx.FilesModel.Update(l.ctx, f); err != nil {
		l.Errorf("update file status failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	resp.Changed = true
	return resp, nil
}
