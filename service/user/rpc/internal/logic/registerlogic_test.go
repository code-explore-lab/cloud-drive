package logic

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"cloud-drive/common/cryptx"
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/types/user"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const insertUserSQL = "insert into users (username, email, password_hash, status) values (?, ?, ?, ?)"
const insertQuotaSQL = "insert into user_quotas (user_id, total_bytes, used_bytes, reserved_bytes) values (?, ?, 0, 0)"

func TestRegister_InvalidArgument(t *testing.T) {
	l := NewRegisterLogic(context.Background(), newTestSvc(newFakeUsersModel(), nil, nil))

	_, err := l.Register(&user.RegisterRequest{Username: "  ", Password: "password123"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}

	_, err = l.Register(&user.RegisterRequest{Username: "alice", Password: "short"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestRegister_UsernameExists(t *testing.T) {
	users := newFakeUsersModel()
	users.findOneByUsername = func(context.Context, string) (*model.Users, error) {
		return &model.Users{Id: 1}, nil
	}

	l := NewRegisterLogic(context.Background(), newTestSvc(users, nil, nil))
	_, err := l.Register(&user.RegisterRequest{Username: "alice", Password: "password123"})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("code = %v, want AlreadyExists", status.Code(err))
	}
}

func TestRegister_EmailExists(t *testing.T) {
	users := newFakeUsersModel()
	users.findOneByEmail = func(context.Context, sql.NullString) (*model.Users, error) {
		return &model.Users{Id: 1}, nil
	}

	l := NewRegisterLogic(context.Background(), newTestSvc(users, nil, nil))
	_, err := l.Register(&user.RegisterRequest{
		Username: "alice",
		Password: "password123",
		Email:    "alice@example.com",
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("code = %v, want AlreadyExists", status.Code(err))
	}
}

func TestRegister_Success(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	svcCtx := newTestSvc(newFakeUsersModel(), nil, nil)
	svcCtx.Conn = sqlx.NewSqlConnFromDB(db)

	passwordHash := cryptx.PasswordEncrypt(testSalt, "password123")

	mock.ExpectBegin()
	mock.ExpectExec(insertUserSQL).
		WithArgs("alice", "alice@example.com", passwordHash, int64(1)).
		WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectExec(insertQuotaSQL).
		WithArgs(int64(7), defaultQuotaBytes).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	l := NewRegisterLogic(context.Background(), svcCtx)
	resp, err := l.Register(&user.RegisterRequest{
		Username: "alice",
		Password: "password123",
		Email:    "alice@example.com",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.UserId != 7 || resp.Username != "alice" {
		t.Fatalf("resp = %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("未满足的 SQL 期望: %v", err)
	}
}

func TestRegister_DuplicateFromDB(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	svcCtx := newTestSvc(newFakeUsersModel(), nil, nil)
	svcCtx.Conn = sqlx.NewSqlConnFromDB(db)

	// 模拟预检查之后、插入之前，另一个请求抢先注册了同名用户
	mock.ExpectBegin()
	mock.ExpectExec(insertUserSQL).
		WithArgs("alice", nil, cryptx.PasswordEncrypt(testSalt, "password123"), int64(1)).
		WillReturnError(errors.New("Error 1062: Duplicate entry 'alice' for key 'uk_users_username'"))
	mock.ExpectRollback()

	l := NewRegisterLogic(context.Background(), svcCtx)
	_, err = l.Register(&user.RegisterRequest{Username: "alice", Password: "password123"})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("code = %v, want AlreadyExists", status.Code(err))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("未满足的 SQL 期望: %v", err)
	}
}
