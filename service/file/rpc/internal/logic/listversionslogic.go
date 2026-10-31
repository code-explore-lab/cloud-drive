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

type ListVersionsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListVersionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListVersionsLogic {
	return &ListVersionsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListVersions 返回文件的全部版本（含当前版本），当前版本用 current 标记。
func (l *ListVersionsLogic) ListVersions(in *file.ListVersionsRequest) (*file.ListVersionsResponse, error) {
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
	if f.OwnerId != uint64(in.UserId) {
		return nil, status.Error(codes.NotFound, "file not found")
	}

	versions, err := l.svcCtx.VersionsModel.FindByFileId(l.ctx, uint64(in.FileId))
	if err != nil {
		l.Errorf("list versions failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	items := make([]*file.VersionItem, 0, len(versions))
	for _, v := range versions {
		items = append(items, &file.VersionItem{
			Version:     int64(v.Version),
			Size:        int64(v.Size),
			ContentType: v.ContentType,
			ContentHash: v.ContentHash,
			CreatedAt:   v.CreatedAt.Unix(),
			Current:     v.Version == f.Version,
		})
	}

	return &file.ListVersionsResponse{Items: items}, nil
}
