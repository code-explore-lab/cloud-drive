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

type GetFileLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFileLogic {
	return &GetFileLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetFile 查询单个文件元数据。只允许文件所有者访问。
func (l *GetFileLogic) GetFile(in *file.GetFileRequest) (*file.GetFileResponse, error) {
	if in.FileId <= 0 || in.UserId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	f, err := l.svcCtx.FilesModel.FindOne(l.ctx, uint64(in.FileId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err != nil {
		l.Errorf("find file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	// 越权检查：已删除的文件对普通查询表现为不存在，避免泄露存在性。
	if f.OwnerId != uint64(in.UserId) || f.DeletedAt.Valid {
		return nil, status.Error(codes.NotFound, "file not found")
	}

	return &file.GetFileResponse{
		FileId:      int64(f.Id),
		OwnerId:     int64(f.OwnerId),
		ParentId:    int64(f.ParentId),
		Name:        f.Name,
		Size:        int64(f.Size),
		ContentType: f.ContentType,
		ContentHash: f.ContentHash,
		ObjectKey:   f.ObjectKey,
		Status:      f.Status,
		Version:     int64(f.Version),
	}, nil
}
