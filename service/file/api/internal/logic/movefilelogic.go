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

type MoveFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMoveFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MoveFileLogic {
	return &MoveFileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// MoveFile 移动文件/目录到目标目录，可选同时改名（改名逻辑与 RenameFile 共用校验）。
func (l *MoveFileLogic) MoveFile(req *types.MoveFileReq) error {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.FileID <= 0 || req.TargetParentID < 0 {
		return status.Error(codes.InvalidArgument, "invalid argument")
	}

	_, err := l.svcCtx.FileRpc.MoveFile(l.ctx, &fileclient.MoveFileRequest{
		FileId:         req.FileID,
		OwnerId:        int64(userId),
		TargetParentId: req.TargetParentID,
		Name:           req.Name,
	})
	return err
}
