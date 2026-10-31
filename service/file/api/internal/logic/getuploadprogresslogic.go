package logic

import (
	"context"

	"cloud-drive/common/ctxdata"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"cloud-drive/service/storage/rpc/storageclient"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetUploadProgressLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUploadProgressLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUploadProgressLogic {
	return &GetUploadProgressLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetUploadProgress 返回上传进度与缺失分片，客户端据此续传。
func (l *GetUploadProgressLogic) GetUploadProgress(req *types.UploadProgressReq) (resp *types.UploadProgressResp, err error) {
	userId, ok := ctxdata.UserIdFromCtx(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if req.UploadID == "" {
		return nil, status.Error(codes.InvalidArgument, "uploadId is required")
	}

	task, err := l.svcCtx.StorageRpc.GetUploadTask(l.ctx, &storageclient.GetUploadTaskRequest{
		UploadId: req.UploadID,
		UserId:   int64(userId),
	})
	if err != nil {
		return nil, err
	}

	uploaded := make(map[int64]struct{}, len(task.Parts))
	parts := make([]types.UploadPartState, 0, len(task.Parts))
	for _, p := range task.Parts {
		number := int64(p.PartNumber)
		uploaded[number] = struct{}{}
		parts = append(parts, types.UploadPartState{
			PartNumber: number,
			Size:       p.Size,
			ETag:       p.Etag,
		})
	}

	missing := make([]int64, 0)
	for n := int64(1); n <= task.TotalChunks; n++ {
		if _, ok := uploaded[n]; !ok {
			missing = append(missing, n)
		}
	}

	return &types.UploadProgressResp{
		UploadID:       task.UploadId,
		Status:         task.Status,
		FileID:         task.FileId,
		FileName:       task.FileName,
		TotalSize:      task.TotalSize,
		ChunkSize:      task.ChunkSize,
		TotalChunks:    task.TotalChunks,
		UploadedChunks: task.UploadedChunks,
		UploadedBytes:  task.UploadedBytes,
		MissingParts:   missing,
		Parts:          parts,
		ExpiresAt:      task.ExpiresAt,
	}, nil
}
