package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListFilesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListFilesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListFilesLogic {
	return &ListFilesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListFilesLogic) ListFiles(req *types.ListFilesReq) (resp *types.ListFilesResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	rpcResp, err := l.svcCtx.FileRpc.ListFiles(l.ctx, &fileclient.ListFilesRequest{
		OwnerId:  int64(userId),
		ParentId: req.ParentID,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		return nil, err
	}

	items := make([]types.FileItem, 0, len(rpcResp.Items))
	for _, item := range rpcResp.Items {
		items = append(items, types.FileItem{
			FileID:      item.FileId,
			ParentID:    item.ParentId,
			Name:        item.Name,
			Size:        item.Size,
			ContentType: item.ContentType,
			IsFolder:    item.IsFolder,
			Status:      item.Status,
			Version:     item.Version,
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
		})
	}

	return &types.ListFilesResp{
		Items:    items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
