package logic

import (
	"context"
	"errors"
	"strings"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AddVersionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAddVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddVersionLogic {
	return &AddVersionLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// AddVersion 把一次上传完成的结果落成新版本。
// 首次上传（version 0 -> 1）和覆盖上传（N -> N+1）走同一条路径，
// 幂等键是上传任务 ID：重复的完成请求只会产生一个版本。
func (l *AddVersionLogic) AddVersion(in *file.AddVersionRequest) (*file.AddVersionResponse, error) {
	if in.FileId <= 0 || in.OwnerId <= 0 || in.Size < 0 || in.ObjectKey == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}
	if strings.TrimSpace(in.RequestId) == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id is required for idempotency")
	}
	if h := in.ContentHash; h != "" && len(h) != 64 {
		return nil, status.Error(codes.InvalidArgument, "content_hash must be a sha256 hex digest")
	}

	version, changed, err := l.svcCtx.VersionsModel.AppendVersion(l.ctx, model.AppendVersionInput{
		FileId:      uint64(in.FileId),
		OwnerId:     uint64(in.OwnerId),
		RequestId:   in.RequestId,
		Size:        uint64(in.Size),
		ContentType: normalizeContentType(in.ContentType),
		ContentHash: in.ContentHash,
		ObjectKey:   in.ObjectKey,
	})
	switch {
	case errors.Is(err, model.ErrNotFound):
		return nil, status.Error(codes.NotFound, "file not found")
	case errors.Is(err, model.ErrPermissionDenied):
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	case err != nil:
		l.Errorf("append version failed: file_id=%d request_id=%s err=%v", in.FileId, in.RequestId, err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	if changed {
		l.Infof("file version published: file_id=%d version=%d size=%d object_key=%s",
			in.FileId, version, in.Size, in.ObjectKey)
	}

	return &file.AddVersionResponse{Version: int64(version), Success: true, Changed: changed}, nil
}
