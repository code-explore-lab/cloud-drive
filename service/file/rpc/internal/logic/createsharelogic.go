package logic

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"cloud-drive/service/file/model"
	"cloud-drive/service/file/rpc/internal/svc"
	"cloud-drive/service/file/rpc/types/file"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CreateShareLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateShareLogic {
	return &CreateShareLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateShare 为文件创建一个分享链接。
//
// 安全要点：token 只用一次随机生成，库里只存 SHA-256；
// 密码用 bcrypt 存储；过期时间与最大访问次数都参与后续访问校验。
func (l *CreateShareLogic) CreateShare(in *file.CreateShareRequest) (*file.CreateShareResponse, error) {
	if in.OwnerId <= 0 || in.FileId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid argument")
	}

	f, err := l.svcCtx.FilesModel.FindOne(l.ctx, uint64(in.FileId))
	if errors.Is(err, model.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if err != nil {
		l.Errorf("find file failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if f.OwnerId != uint64(in.OwnerId) || f.DeletedAt.Valid {
		return nil, status.Error(codes.NotFound, "file not found")
	}
	if f.Status != "available" {
		return nil, status.Error(codes.FailedPrecondition, "file is not ready to be shared")
	}

	var expiresAt sql.NullTime
	if in.ExpiresAt > 0 {
		t := time.Unix(in.ExpiresAt, 0)
		if !t.After(time.Now()) {
			return nil, status.Error(codes.InvalidArgument, "expires_at must be in the future")
		}
		expiresAt = sql.NullTime{Time: t, Valid: true}
	}

	passwordHash, err := hashSharePassword(in.Password)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid share password")
	}

	token, err := newShareToken()
	if err != nil {
		l.Errorf("generate share token failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	res, err := l.svcCtx.SharesModel.Insert(l.ctx, &model.Shares{
		FileId:         f.Id,
		OwnerId:        f.OwnerId,
		TokenHash:      hashShareToken(token),
		PasswordHash:   sql.NullString{String: passwordHash, Valid: passwordHash != ""},
		ExpiresAt:      expiresAt,
		MaxAccessCount: sql.NullInt64{Int64: in.MaxAccessCount, Valid: in.MaxAccessCount > 0},
		Status:         1,
	})
	if err != nil {
		l.Errorf("insert share failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	id, err := res.LastInsertId()
	if err != nil {
		l.Errorf("get last insert id failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	l.Infof("share created: share_id=%d file_id=%d owner_id=%d has_password=%v max_access=%d",
		id, f.Id, f.OwnerId, passwordHash != "", in.MaxAccessCount)

	return &file.CreateShareResponse{
		ShareId:   id,
		Token:     token,
		ExpiresAt: in.ExpiresAt,
	}, nil
}
