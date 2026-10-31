package logic

import (
	"context"
	"errors"

	"cloud-drive/service/storage/model"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetObjectInfoLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetObjectInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetObjectInfoLogic {
	return &GetObjectInfoLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetObjectInfoLogic) GetObjectInfo(in *storage.GetObjectInfoRequest) (*storage.GetObjectInfoResponse, error) {
	if in.ObjectKey == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	obj, err := l.svcCtx.ObjectsModel.FindOneByObjectKey(l.ctx, in.ObjectKey)
	if errors.Is(err, model.ErrNotFound) {
		return &storage.GetObjectInfoResponse{ObjectKey: in.ObjectKey, Exists: false}, nil
	}
	if err != nil {
		l.Errorf("find storage object failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	// 只有 available 的对象才对下载方可见；deleting/deleted/failed 一律视为不存在。
	if obj.Status != "available" {
		return &storage.GetObjectInfoResponse{ObjectKey: in.ObjectKey, Exists: false}, nil
	}

	return &storage.GetObjectInfoResponse{
		ObjectKey:   obj.ObjectKey,
		Size:        int64(obj.Size),
		ContentType: obj.ContentType,
		ContentHash: obj.ContentHash,
		Exists:      true,
	}, nil
}
