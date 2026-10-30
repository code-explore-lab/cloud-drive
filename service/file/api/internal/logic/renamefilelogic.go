// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package logic

import (
	"context"

	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RenameFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRenameFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RenameFileLogic {
	return &RenameFileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RenameFileLogic) RenameFile(req *types.RenameFileReq) error {
	// todo: add your logic here and delete this line

	return nil
}
