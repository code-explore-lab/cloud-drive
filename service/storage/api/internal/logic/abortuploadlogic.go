// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package logic

import (
	"context"

	"cloud-drive/service/storage/api/internal/svc"
	"cloud-drive/service/storage/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AbortUploadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAbortUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AbortUploadLogic {
	return &AbortUploadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AbortUploadLogic) AbortUpload() (resp *types.AbortUploadResp, err error) {
	// todo: add your logic here and delete this line

	return
}
