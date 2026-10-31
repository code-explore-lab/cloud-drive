package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DeleteFolderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteFolderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFolderLogic {
	return &DeleteFolderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DeleteFolder 递归删除目录，并清理其中文件的对象与配额。
func (l *DeleteFolderLogic) DeleteFolder(req *types.FolderIDReq) (resp *types.DeleteFileResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.FolderID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid folder id")
	}

	requestId := uuid.NewString()
	rpcResp, err := l.svcCtx.FileRpc.DeleteFolder(l.ctx, &fileclient.DeleteFolderRequest{
		FolderId:  req.FolderID,
		OwnerId:   int64(userId),
		RequestId: requestId,
	})
	if err != nil {
		return nil, err
	}

	if rpcResp.Changed {
		cleanupObjects(l.ctx, l.svcCtx, rpcResp.Objects, requestId)
		releaseUsed(l.ctx, l.svcCtx, int64(userId), rpcResp.ReleasedBytes, requestId)
		l.Infof("folder deleted: folder_id=%d objects=%d released_bytes=%d",
			req.FolderID, len(rpcResp.Objects), rpcResp.ReleasedBytes)
	}

	return &types.DeleteFileResp{Success: rpcResp.Success, ReleasedBytes: rpcResp.ReleasedBytes}, nil
}
