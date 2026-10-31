package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/file/rpc/fileclient"
	"cloud-drive/service/storage/rpc/storageclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CompleteUploadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCompleteUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CompleteUploadLogic {
	return &CompleteUploadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CompleteUpload 编排上传完成：
//  1. storage 合并分片、校验大小与摘要，返回对象 Key；
//  2. file 服务把对象登记为新版本（幂等键是 uploadId）；
//  3. 只有版本真的变了，才把预占额度转成已用额度。
//
// 第 3 步的幂等很重要：客户端重试完成请求时不能重复扣配额。
func (l *CompleteUploadLogic) CompleteUpload(req *types.CompleteUploadReq) (resp *types.CompleteUploadResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.UploadID == "" {
		return nil, status.Error(codes.InvalidArgument, "uploadId is required")
	}

	storageResp, err := l.svcCtx.StorageRpc.CompleteUpload(l.ctx, &storageclient.CompleteUploadRequest{
		UploadId:  req.UploadID,
		UserId:    int64(userId),
		RequestId: req.UploadID,
	})
	if err != nil {
		return nil, err
	}
	if !storageResp.Success {
		return nil, status.Error(codes.Internal, "storage complete upload failed")
	}

	f, err := l.svcCtx.FileRpc.GetFile(l.ctx, &fileclient.GetFileRequest{
		FileId: storageResp.FileId,
		UserId: int64(userId),
	})
	if err != nil {
		return nil, err
	}

	versionResp, err := l.svcCtx.FileRpc.AddVersion(l.ctx, &fileclient.AddVersionRequest{
		FileId:      storageResp.FileId,
		OwnerId:     int64(userId),
		Size:        storageResp.Size,
		ContentType: f.ContentType,
		ContentHash: storageResp.ContentHash,
		ObjectKey:   storageResp.ObjectKey,
		RequestId:   req.UploadID,
	})
	if err != nil {
		return nil, err
	}

	if versionResp.Changed {
		commitQuota(l.ctx, l.svcCtx, int64(userId), storageResp.Size, req.UploadID)
	}

	l.Infof("upload completed: upload_id=%s file_id=%d version=%d size=%d verified=%v",
		req.UploadID, storageResp.FileId, versionResp.Version, storageResp.Size, storageResp.HashVerified)

	return &types.CompleteUploadResp{
		FileID:  storageResp.FileId,
		Version: versionResp.Version,
		Status:  "available",
	}, nil
}
