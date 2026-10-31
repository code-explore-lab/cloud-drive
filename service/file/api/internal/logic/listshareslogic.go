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

type ListSharesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListSharesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSharesLogic {
	return &ListSharesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListSharesLogic) ListShares(req *types.ListSharesReq) (resp *types.ListSharesResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	rpcResp, err := l.svcCtx.FileRpc.ListShares(l.ctx, &fileclient.ListSharesRequest{
		OwnerId:  int64(userId),
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		return nil, err
	}

	items := make([]types.ShareItem, 0, len(rpcResp.Items))
	for _, s := range rpcResp.Items {
		items = append(items, types.ShareItem{
			ShareID:        s.ShareId,
			FileID:         s.FileId,
			FileName:       s.FileName,
			Active:         s.Active,
			HasPassword:    s.HasPassword,
			ExpiresAt:      s.ExpiresAt,
			MaxAccessCount: s.MaxAccessCount,
			AccessCount:    s.AccessCount,
			CreatedAt:      s.CreatedAt,
		})
	}

	return &types.ListSharesResp{
		Items:    items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
