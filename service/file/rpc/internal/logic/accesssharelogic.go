package logic

import (
	"context"
	"errors"
	"time"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// timeNow 通过函数变量暴露当前时间，便于单元测试控制过期行为。
var timeNow = time.Now

type AccessShareLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAccessShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AccessShareLogic {
	return &AccessShareLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// AccessShare 校验分享是否可访问，并原子地占用一次访问配额。
//
// 返回 granted=false + reason 而不是 gRPC 错误：
// 分享失效、密码错误、次数用尽都是"正常的访问拒绝"，不属于服务端错误。
func (l *AccessShareLogic) AccessShare(in *file.AccessShareRequest) (*file.AccessShareResponse, error) {
	if in.Token == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	share, err := l.svcCtx.SharesModel.FindOneByTokenHash(l.ctx, hashShareToken(in.Token))
	if errors.Is(err, model.ErrNotFound) {
		return &file.AccessShareResponse{Granted: false, Reason: "share not found"}, nil
	}
	if err != nil {
		l.Errorf("find share by token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if share.Status == 0 {
		return &file.AccessShareResponse{Granted: false, Reason: "share was revoked"}, nil
	}
	if expired(share) {
		return &file.AccessShareResponse{Granted: false, Reason: "share has expired"}, nil
	}
	if share.MaxAccessCount.Valid && int64(share.AccessCount) >= share.MaxAccessCount.Int64 {
		return &file.AccessShareResponse{Granted: false, Reason: "share access limit reached"}, nil
	}
	if !verifySharePassword(share.PasswordHash.String, in.Password) {
		return &file.AccessShareResponse{Granted: false, Reason: "invalid share password"}, nil
	}

	// 条件更新兜住并发：两次请求同时通过上面的检查时，这里只会有一次真正加计数。
	ok, err := l.svcCtx.SharesModel.IncrementAccess(l.ctx, share, timeNow())
	if err != nil {
		l.Errorf("increment share access failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !ok {
		return &file.AccessShareResponse{Granted: false, Reason: "share is no longer available"}, nil
	}

	f, err := l.svcCtx.FilesModel.FindOne(l.ctx, share.FileId)
	if errors.Is(err, model.ErrNotFound) {
		return &file.AccessShareResponse{Granted: false, Reason: "file not found"}, nil
	}
	if err != nil {
		l.Errorf("find shared file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if f.DeletedAt.Valid || f.Status != "available" {
		return &file.AccessShareResponse{Granted: false, Reason: "file is not available"}, nil
	}

	return &file.AccessShareResponse{
		Granted:     true,
		ShareId:     int64(share.Id),
		FileId:      int64(f.Id),
		OwnerId:     int64(f.OwnerId),
		FileName:    f.Name,
		Size:        int64(f.Size),
		ContentType: f.ContentType,
		ObjectKey:   f.ObjectKey,
		Status:      f.Status,
	}, nil
}
