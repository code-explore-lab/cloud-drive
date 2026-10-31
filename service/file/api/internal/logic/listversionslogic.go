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

type ListVersionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListVersionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListVersionsLogic {
	return &ListVersionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListVersionsLogic) ListVersions(req *types.ListVersionsReq) (resp *types.ListVersionsResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.FileID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid file id")
	}

	rpcResp, err := l.svcCtx.FileRpc.ListVersions(l.ctx, &fileclient.ListVersionsRequest{
		FileId: req.FileID,
		UserId: int64(userId),
	})
	if err != nil {
		return nil, err
	}

	items := make([]types.VersionItem, 0, len(rpcResp.Items))
	for _, v := range rpcResp.Items {
		items = append(items, types.VersionItem{
			Version:     v.Version,
			Size:        v.Size,
			ContentType: v.ContentType,
			ContentHash: v.ContentHash,
			Current:     v.Current,
			CreatedAt:   v.CreatedAt,
		})
	}
	return &types.ListVersionsResp{Items: items}, nil
}
