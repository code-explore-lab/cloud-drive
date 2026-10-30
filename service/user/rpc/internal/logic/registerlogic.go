package logic

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"cloud-drive/common/cryptx"
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/internal/svc"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RegisterLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ---- 对外：由 user-api 调用 ----
func (l *RegisterLogic) Register(in *user.RegisterRequest) (*user.RegisterResponse, error) {
	//judge dup
	username := strings.TrimSpace(in.Username)
	if username == "" || len(in.Password) < 8 {
		return nil, status.Error(codes.InvalidArgument, "username and password are required")
	}
	email := sql.NullString{String: strings.TrimSpace(in.Email), Valid: strings.TrimSpace(in.Email) != ""}

	// 存在性预检查：只为了返回友好错误，并发正确性靠唯一索引
	if _, err := l.svcCtx.UsersModel.FindOneByUsername(l.ctx, username); err == nil {
		return nil, status.Error(codes.AlreadyExists, "username already exists")
	} else if !errors.Is(err, model.ErrNotFound) {
		l.Errorf("find user by username failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if email.Valid {
		if _, err := l.svcCtx.UsersModel.FindOneByEmail(l.ctx, email); err == nil {
			return nil, status.Error(codes.AlreadyExists, "email already exists")
		} else if !errors.Is(err, model.ErrNotFound) {
			l.Errorf("find user by email failed: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
	}

	passwordHash := cryptx.PasswordEncrypt(l.svcCtx.Config.Salt, in.Password)

	var userId uint64
	err := l.svcCtx.Conn.TransactCtx(l.ctx, func(ctx context.Context, session sqlx.Session) error {
		res, err := session.ExecCtx(ctx,
			"insert into users (username, email, password_hash, status) values (?, ?, ?, ?)",
			username, email, passwordHash, 1,
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		userId = uint64(id)

		_, err = session.ExecCtx(ctx,
			"insert into user_quotas (user_id, total_bytes, used_bytes, reserved_bytes) values (?, ?, 0, 0)",
			userId, defaultQuotaBytes,
		)
		return err
	})
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			return nil, status.Error(codes.AlreadyExists, "username or email already exists")
		}
		l.Errorf("create user failed: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &user.RegisterResponse{UserId: int64(userId), Username: username}, nil
}
