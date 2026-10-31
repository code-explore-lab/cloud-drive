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

type RenameFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRenameFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RenameFileLogic {
	return &RenameFileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RenameFileLogic) RenameFile(req *types.RenameFileReq) error {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "unauthorized")
	}

	_, err := l.svcCtx.FileRpc.RenameFile(l.ctx, &fileclient.RenameFileRequest{
		FileId:  req.FileID,
		OwnerId: int64(userId),
		Name:    req.Name,
	})
	return err
}
