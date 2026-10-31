package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"
	"cloud-drive/service/storage/rpc/storageclient"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DeleteFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteFileLogic {
	return &DeleteFileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DeleteFile 编排文件删除：
//  1. file 服务软删除并返回需要清理的对象清单（含历史版本）；
//  2. 归还已用配额；
//  3. 逐个删除存储对象（失败只记录，由存储侧 GC 重试）。
//
// 顺序刻意如此：先让文件在业务上不可见，再做外部清理。
// 反过来会出现"对象删了但记录还在"的不可恢复状态。
func (l *DeleteFileLogic) DeleteFile(req *types.FileIDReq) (resp *types.DeleteFileResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.FileID <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid file id")
	}

	requestId := uuid.NewString()
	rpcResp, err := l.svcCtx.FileRpc.DeleteFile(l.ctx, &fileclient.DeleteFileRequest{
		FileId:    req.FileID,
		OwnerId:   int64(userId),
		RequestId: requestId,
	})
	if err != nil {
		return nil, err
	}

	if rpcResp.Changed {
		cleanupObjects(l.ctx, l.svcCtx, rpcResp.Objects, requestId)
		releaseUsed(l.ctx, l.svcCtx, int64(userId), rpcResp.ReleasedBytes, requestId)
		l.Infof("file deleted: file_id=%d objects=%d released_bytes=%d",
			req.FileID, len(rpcResp.Objects), rpcResp.ReleasedBytes)
	}

	return &types.DeleteFileResp{Success: rpcResp.Success, ReleasedBytes: rpcResp.ReleasedBytes}, nil
}

// cleanupObjects 删除存储对象。DeleteObject 自己会把失败的对象标记成 deleting 交给
// 后台 GC 重试，所以这里只记日志，不向上抛错——文件在业务上已经删掉了。
func cleanupObjects(ctx context.Context, svcCtx *svc.ServiceContext,
	objects []*fileclient.ObjectRef, requestId string) {
	for _, obj := range objects {
		if obj.ObjectKey == "" {
			continue
		}
		if _, err := svcCtx.StorageRpc.DeleteObject(ctx, &storageclient.DeleteObjectRequest{
			ObjectKey: obj.ObjectKey,
			RequestId: requestId,
		}); err != nil {
			logx.WithContext(ctx).Errorf("delete object %s failed: %v", obj.ObjectKey, err)
		}
	}
}
