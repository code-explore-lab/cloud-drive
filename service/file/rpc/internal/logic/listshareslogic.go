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

type ListSharesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListSharesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSharesLogic {
	return &ListSharesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListShares 分页返回当前用户创建的分享，包含已失效的，便于前端展示状态。
func (l *ListSharesLogic) ListShares(in *file.ListSharesRequest) (*file.ListSharesResponse, error) {
	if in.OwnerId <= 0 {
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
	shares, err := l.svcCtx.SharesModel.FindByOwner(l.ctx, ownerId, (page-1)*pageSize, pageSize)
	if err != nil {
		l.Errorf("list shares failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	total, err := l.svcCtx.SharesModel.CountByOwner(l.ctx, ownerId)
	if err != nil {
		l.Errorf("count shares failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	items := make([]*file.ShareItem, 0, len(shares))
	for _, s := range shares {
		item := &file.ShareItem{
			ShareId:     int64(s.Id),
			FileId:      int64(s.FileId),
			Active:      s.Status == 1 && !expired(s),
			ExpiresAt:   s.ExpiresAt.Time.Unix(),
			AccessCount: int64(s.AccessCount),
			CreatedAt:   s.CreatedAt.Unix(),
		}
		if !s.ExpiresAt.Valid {
			item.ExpiresAt = 0
		}
		if s.MaxAccessCount.Valid {
			item.MaxAccessCount = s.MaxAccessCount.Int64
		}
		item.HasPassword = s.PasswordHash.Valid && s.PasswordHash.String != ""

		if f, err := l.svcCtx.FilesModel.FindOne(l.ctx, s.FileId); err == nil {
			item.FileName = f.Name
			if f.DeletedAt.Valid {
				item.Active = false
			}
		} else if !errors.Is(err, model.ErrNotFound) {
			l.Errorf("find shared file failed: %v", err)
		}
		items = append(items, item)
	}

	return &file.ListSharesResponse{Items: items, Total: total}, nil
}

func expired(s *model.Shares) bool {
	return s.ExpiresAt.Valid && !s.ExpiresAt.Time.After(timeNow())
}
