package logic

import (
	"context"

	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListFilesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListFilesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListFilesLogic {
	return &ListFilesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListFiles 列出某个目录下的直接子项：目录全量返回、文件按页返回，目录排在文件前面。
// 分页上限固定为 100，避免客户端一次拉走整棵树。
func (l *ListFilesLogic) ListFiles(in *file.ListFilesRequest) (*file.ListFilesResponse, error) {
	if in.OwnerId <= 0 || in.ParentId < 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	page := in.Page
	if page < 1 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	ownerId := uint64(in.OwnerId)
	parentId := uint64(in.ParentId)

	folders, err := l.svcCtx.FoldersModel.FindByOwnerParent(l.ctx, ownerId, parentId)
	if err != nil {
		l.Errorf("list folders failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	folderCount, err := l.svcCtx.FoldersModel.CountByOwnerParent(l.ctx, ownerId, parentId)
	if err != nil {
		l.Errorf("count folders failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	offset := (page - 1) * pageSize
	files, err := l.svcCtx.FilesModel.FindByOwnerParent(l.ctx, ownerId, parentId, offset, pageSize)
	if err != nil {
		l.Errorf("list files failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	fileCount, err := l.svcCtx.FilesModel.CountByOwnerParent(l.ctx, ownerId, parentId)
	if err != nil {
		l.Errorf("count files failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	items := make([]*file.FileItem, 0, len(folders)+len(files))
	for _, folder := range folders {
		items = append(items, &file.FileItem{
			FileId:    int64(folder.Id),
			ParentId:  int64(folder.ParentId),
			Name:      folder.Name,
			IsFolder:  true,
			CreatedAt: folder.CreatedAt.Unix(),
			UpdatedAt: folder.UpdatedAt.Unix(),
		})
	}
	for _, f := range files {
		items = append(items, &file.FileItem{
			FileId:      int64(f.Id),
			ParentId:    int64(f.ParentId),
			Name:        f.Name,
			Size:        int64(f.Size),
			ContentType: f.ContentType,
			IsFolder:    false,
			Status:      f.Status,
			CreatedAt:   f.CreatedAt.Unix(),
			UpdatedAt:   f.UpdatedAt.Unix(),
			Version:     int64(f.Version),
		})
	}

	return &file.ListFilesResponse{
		Items: items,
		Total: folderCount + fileCount,
	}, nil
}
