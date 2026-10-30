// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package logic

import (
	"context"

	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListFilesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListFilesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListFilesLogic {
	return &ListFilesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListFilesLogic) ListFiles(req *types.ListFilesReq) (resp *types.ListFilesResp, err error) {
	// todo: add your logic here and delete this line

	return
}
