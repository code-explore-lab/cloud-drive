// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package logic

import (
	"context"

	"cloud-drive/service/storage/api/internal/svc"
	"cloud-drive/service/storage/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
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

func (l *InitUploadLogic) InitUpload(req *types.InitStorageUploadReq) (resp *types.InitStorageUploadResp, err error) {
	// todo: add your logic here and delete this line

	return
}
