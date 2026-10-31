package logic

import (
	"context"
	"errors"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RevokeShareLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRevokeShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RevokeShareLogic {
	return &RevokeShareLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RevokeShare 撤销分享。只有分享的创建者可以撤销；重复撤销是幂等的。
func (l *RevokeShareLogic) RevokeShare(in *file.RevokeShareRequest) (*file.RevokeShareResponse, error) {
	if in.ShareId <= 0 || in.OwnerId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	share, err := l.svcCtx.SharesModel.FindOne(l.ctx, uint64(in.ShareId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "share not found")
	}
	if err != nil {
		l.Errorf("find share failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	// 不区分"不存在"和"不是你的"，避免暴露其他用户的 share id。
	if share.OwnerId != uint64(in.OwnerId) {
		return nil, status.Error(codes.NotFound, "share not found")
	}
	if share.Status == 0 {
		return &file.RevokeShareResponse{Success: true}, nil
	}

	if _, err := l.svcCtx.SharesModel.Revoke(l.ctx, share); err != nil {
		l.Errorf("revoke share failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	l.Infof("share revoked: share_id=%d owner_id=%d", share.Id, share.OwnerId)
	return &file.RevokeShareResponse{Success: true}, nil
}
