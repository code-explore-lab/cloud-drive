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

type InitUploadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewInitUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *InitUploadLogic {
	return &InitUploadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// InitUpload 编排一次上传：
//  1. 先建文件元数据（uploading 状态，带幂等键）；
//  2. 首次创建时预占配额，额度不足直接拒绝；
//  3. 让 storage 创建上传会话，返回分片参数。
//
// 只有前三步都成功才返回给客户端，避免出现"客户端以为在传、服务端没记录"的状态。
func (l *InitUploadLogic) InitUpload(req *types.InitUploadReq) (resp *types.InitUploadResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.Size < 0 {
		return nil, status.Error(codes.InvalidArgument, "size must not be negative")
	}
	if max := l.svcCtx.Config.Upload.MaxFileBytes; max > 0 && req.Size > max {
		return nil, status.Errorf(codes.ResourceExhausted,
			"file too large: %d bytes exceeds limit %d", req.Size, max)
	}

	requestId, err := normalizeRequestId(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if requestId == "" {
		// 没有幂等键时用随机 UUID：仍然保证"一次请求一个上传任务"。
		requestId = uuid.NewString()
	}

	fileResp, err := l.svcCtx.FileRpc.CreateFile(l.ctx, &fileclient.CreateFileRequest{
		OwnerId:     int64(userId),
		ParentId:    req.ParentID,
		Name:        req.Name,
		Size:        req.Size,
		ContentType: req.ContentType,
		ContentHash: req.ContentHash,
		RequestId:   requestId,
		Status:      "uploading",
	})
	if err != nil {
		return nil, err
	}

	if !fileResp.Existed {
		if err := reserveQuota(l.ctx, l.svcCtx, int64(userId), req.Size, requestId); err != nil {
			// 额度不足：把刚建的文件标记为失败，避免它一直悬在 uploading。
			l.markFailed(fileResp.FileId, int64(userId))
			return nil, err
		}
	}

	storageResp, err := l.svcCtx.StorageRpc.InitUpload(l.ctx, &storageclient.InitUploadRequest{
		UploadId:    requestId,
		UserId:      int64(userId),
		FileName:    req.Name,
		Size:        req.Size,
		ContentType: req.ContentType,
		ContentHash: req.ContentHash,
		FileId:      fileResp.FileId,
	})
	if err != nil {
		if !fileResp.Existed {
			releaseReserved(l.ctx, l.svcCtx, int64(userId), req.Size, requestId)
		}
		l.markFailed(fileResp.FileId, int64(userId))
		return nil, err
	}

	l.Infof("upload initialized: upload_id=%s file_id=%d user_id=%d size=%d chunks=%d",
		storageResp.UploadId, fileResp.FileId, userId, req.Size, storageResp.TotalChunks)

	return &types.InitUploadResp{
		UploadID:    storageResp.UploadId,
		FileID:      fileResp.FileId,
		ChunkSize:   storageResp.ChunkSize,
		TotalChunks: storageResp.TotalChunks,
		ExpiresAt:   storageResp.ExpiresAt,
	}, nil
}

func (l *InitUploadLogic) markFailed(fileId, userId int64) {
	if fileId <= 0 {
		return
	}
	if _, err := l.svcCtx.FileRpc.UpdateFileStatus(l.ctx, &fileclient.UpdateFileStatusRequest{
		FileId:  fileId,
		OwnerId: userId,
		Status:  "failed",
	}); err != nil {
		l.Errorf("mark file %d as failed: %v", fileId, err)
	}
}
