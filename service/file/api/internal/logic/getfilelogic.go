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

type GetFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFileLogic {
	return &GetFileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetFileLogic) GetFile(req *types.FileIDReq) (resp *types.GetFileResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	rpcResp, err := l.svcCtx.FileRpc.GetFile(l.ctx, &fileclient.GetFileRequest{
		FileId: req.FileID,
		UserId: int64(userId),
	})
	if err != nil {
		return nil, err
	}

	return &types.GetFileResp{
		File: types.FileItem{
			FileID:      rpcResp.FileId,
			ParentID:    rpcResp.ParentId,
			Name:        rpcResp.Name,
			Size:        rpcResp.Size,
			ContentType: rpcResp.ContentType,
			IsFolder:    false,
			Status:      rpcResp.Status,
			Version:     rpcResp.Version,
		},
	}, nil
}
