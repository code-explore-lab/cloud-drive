package logic

import (
	"context"
	"errors"

	"cloud-drive/common/objectstore"
	"cloud-drive/service/storage/rpc/internal/svc"
	"cloud-drive/service/storage/rpc/types/storage"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CheckObjectLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCheckObjectLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CheckObjectLogic {
	return &CheckObjectLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CheckObject 用于对账：直接问对象存储"这个 Key 还在不在、内容对不对"，
// 不依赖数据库里的元数据，因此能发现"元数据说在、对象已经丢了"的情况。
func (l *CheckObjectLogic) CheckObject(in *storage.CheckObjectRequest) (*storage.CheckObjectResponse, error) {
	if in.ObjectKey == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	info, err := l.svcCtx.Store.Stat(l.ctx, in.ObjectKey)
	if errors.Is(err, objectstore.ErrNotFound) {
		return &storage.CheckObjectResponse{Exists: false}, nil
	}
	if err != nil {
		l.Errorf("stat object failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &storage.CheckObjectResponse{
		Exists: true,
		Size:   info.Size,
	}
	if in.ExpectedHash != "" {
		hash, err := l.svcCtx.Store.SHA256(l.ctx, in.ObjectKey)
		if err != nil {
			l.Errorf("hash object failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
		resp.ContentHash = hash
		resp.HashMatch = hash == in.ExpectedHash
	}
	return resp, nil
}
