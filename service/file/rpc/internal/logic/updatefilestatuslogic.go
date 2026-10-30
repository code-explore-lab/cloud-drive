package logic

import (
	"context"

	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateFileStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateFileStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateFileStatusLogic {
	return &UpdateFileStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateFileStatusLogic) UpdateFileStatus(in *file.UpdateFileStatusRequest) (*file.UpdateFileStatusResponse, error) {
	// todo: add your logic here and delete this line

	return &file.UpdateFileStatusResponse{}, nil
}
