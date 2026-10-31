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

type RenameFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRenameFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RenameFileLogic {
	return &RenameFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RenameFile 修改文件名（如传入的 fileId 实际是目录，则改目录名）。
func (l *RenameFileLogic) RenameFile(in *file.RenameFileRequest) (*file.RenameFileResponse, error) {
	if in.FileId <= 0 || in.OwnerId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}
	name, err := validateName(in.Name)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid name")
	}

	ownerId := uint64(in.OwnerId)

	// 文件/目录共用一个 ID 空间之外的入口：先当文件查，查不到再当目录查。
	f, err := l.svcCtx.FilesModel.FindOne(l.ctx, uint64(in.FileId))
	if err == nil {
		if f.OwnerId != ownerId {
			return nil, status.Error(codes.NotFound, "file not found")
		}
		if f.DeletedAt.Valid {
			return nil, status.Error(codes.NotFound, "file not found")
		}
		if err := ensureNameAvailable(l.ctx, l.svcCtx, ownerId, f.ParentId, name, f.Id); err != nil {
			return nil, err
		}
		f.Name = name
		if err := l.svcCtx.FilesModel.Update(l.ctx, f); err != nil {
			l.Errorf("update file failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
		return &file.RenameFileResponse{Success: true}, nil
	}
	if !errors.Is(err, model.ErrNotFound) {
		l.Errorf("find file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	folder, err := l.svcCtx.FoldersModel.FindOne(l.ctx, uint64(in.FileId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err != nil {
		l.Errorf("find folder failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if folder.OwnerId != ownerId || folder.DeletedAt.Valid {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err := ensureNameAvailable(l.ctx, l.svcCtx, ownerId, folder.ParentId, name, 0); err != nil {
		return nil, err
	}
	folder.Name = name
	if err := l.svcCtx.FoldersModel.Update(l.ctx, folder); err != nil {
		l.Errorf("update folder failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &file.RenameFileResponse{Success: true}, nil
}
