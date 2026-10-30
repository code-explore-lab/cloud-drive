// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package logic

import (
	"context"

	"cloud-drive/service/storage/api/internal/svc"
	"cloud-drive/service/storage/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetObjectLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetObjectLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetObjectLogic {
	return &GetObjectLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetObjectLogic) GetObject() (resp *types.GetStorageObjectResp, err error) {
	// todo: add your logic here and delete this line

	return
}
