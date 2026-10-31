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

type InitVersionUploadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewInitVersionUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *InitVersionUploadLogic {
	return &InitVersionUploadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// InitVersionUpload 为已存在的文件创建一个"新版本"上传会话。
// 上传完成后仍然走同一个 CompleteUpload 接口：storage 返回同一个 fileId，
// file 服务会把它追加成 version+1。
func (l *InitVersionUploadLogic) InitVersionUpload(req *types.InitVersionUploadReq) (resp *types.InitUploadResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.FileID <= 0 || req.Size < 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}
	if max := l.svcCtx.Config.Upload.MaxFileBytes; max > 0 && req.Size > max {
		return nil, status.Errorf(codes.ResourceExhausted,
			"file too large: %d bytes exceeds limit %d", req.Size, max)
	}

	f, err := l.svcCtx.FileRpc.GetFile(l.ctx, &fileclient.GetFileRequest{
		FileId: req.FileID,
		UserId: int64(userId),
	})
	if err != nil {
		return nil, err
	}
	if f.Status != "available" {
		return nil, status.Error(codes.FailedPrecondition, "file is not ready for a new version")
	}

	uploadId := uuid.NewString()
	if err := reserveQuota(l.ctx, l.svcCtx, int64(userId), req.Size, uploadId); err != nil {
		return nil, err
	}

	contentType := req.ContentType
	if contentType == "" {
		contentType = f.ContentType
	}

	storageResp, err := l.svcCtx.StorageRpc.InitUpload(l.ctx, &storageclient.InitUploadRequest{
		UploadId:    uploadId,
		UserId:      int64(userId),
		FileName:    f.Name,
		Size:        req.Size,
		ContentType: contentType,
		ContentHash: req.ContentHash,
		FileId:      f.FileId,
	})
	if err != nil {
		releaseReserved(l.ctx, l.svcCtx, int64(userId), req.Size, uploadId)
		return nil, err
	}

	return &types.InitUploadResp{
		UploadID:    storageResp.UploadId,
		FileID:      f.FileId,
		ChunkSize:   storageResp.ChunkSize,
		TotalChunks: storageResp.TotalChunks,
		ExpiresAt:   storageResp.ExpiresAt,
	}, nil
}
