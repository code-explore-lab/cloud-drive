package logic

import (
	"context"
	"errors"
	"strings"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RestoreVersionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRestoreVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RestoreVersionLogic {
	return &RestoreVersionLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RestoreVersion 把文件内容回滚到指定版本。
//
// 实现方式不是"改写历史"，而是"追加一个新版本"：被回滚到的内容会成为新的当前版本，
// 原来的当前版本仍然保留在版本历史里。版本表因此是只追加的，审计语义不会被破坏。
func (l *RestoreVersionLogic) RestoreVersion(in *file.RestoreVersionRequest) (*file.RestoreVersionResponse, error) {
	if in.FileId <= 0 || in.UserId <= 0 || in.Version <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}
	if strings.TrimSpace(in.RequestId) == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id is required for idempotency")
	}

	f, err := l.svcCtx.FilesModel.FindOne(l.ctx, uint64(in.FileId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err != nil {
		l.Errorf("find file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if f.OwnerId != uint64(in.UserId) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if f.DeletedAt.Valid {
		return nil, status.Error(codes.NotFound, "file not found")
	}

	target, err := l.svcCtx.VersionsModel.FindOneByFileIdVersion(l.ctx, uint64(in.FileId), uint64(in.Version))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "version not found")
	}
	if err != nil {
		l.Errorf("find version failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	// 已经是当前内容时不需要再追加版本，直接返回当前版本号。
	if target.ObjectKey == f.ObjectKey && f.Status == "available" {
		return &file.RestoreVersionResponse{Success: true, Changed: false, Version: int64(f.Version)}, nil
	}

	newVersion, changed, err := l.svcCtx.VersionsModel.AppendVersion(l.ctx, model.AppendVersionInput{
		FileId:      uint64(in.FileId),
		OwnerId:     uint64(in.UserId),
		RequestId:   in.RequestId,
		Size:        target.Size,
		ContentType: target.ContentType,
		ContentHash: target.ContentHash,
		ObjectKey:   target.ObjectKey,
	})
	switch {
	case errors.Is(err, model.ErrNotFound):
		return nil, status.Error(codes.NotFound, "file not found")
	case errors.Is(err, model.ErrPermissionDenied):
		return nil, status.Error(codes.NotFound, "file not found")
	case err != nil:
		l.Errorf("restore version failed: file_id=%d version=%d err=%v", in.FileId, in.Version, err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	if changed {
		l.Infof("file version restored: file_id=%d from_version=%d new_version=%d",
			in.FileId, in.Version, newVersion)
	}
	return &file.RestoreVersionResponse{Success: true, Changed: changed, Version: int64(newVersion)}, nil
}
