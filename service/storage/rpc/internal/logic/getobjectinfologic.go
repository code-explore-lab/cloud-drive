package logic

import (
	"context"

	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
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
	// todo: add your logic here and delete this line

	return &storage.GetObjectInfoResponse{}, nil
}
