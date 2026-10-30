package logic

import (
	"context"

	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteObjectLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteObjectLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteObjectLogic {
	return &DeleteObjectLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteObjectLogic) DeleteObject(in *storage.DeleteObjectRequest) (*storage.DeleteObjectResponse, error) {
	// todo: add your logic here and delete this line

	return &storage.DeleteObjectResponse{}, nil
}
