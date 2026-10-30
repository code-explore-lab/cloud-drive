package logic

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/types/user"
)

func TestGetUser_Success(t *testing.T) {
	created := time.Unix(1700000000, 0)
	users := newFakeUsersModel()
	users.findOne = func(_ context.Context, id uint64) (*model.Users, error) {
		return &model.Users{
			Id:        id,
			Username:  "alice",
			Email:     sql.NullString{String: "alice@example.com", Valid: true},
			Status:    1,
			CreatedAt: created,
		}, nil
	}

	l := NewGetUserLogic(context.Background(), newTestSvc(users, nil, nil))
	resp, err := l.GetUser(&user.GetUserRequest{UserId: 42})
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}

	if resp.UserId != 42 {
		t.Fatalf("UserId = %d, want 42", resp.UserId)
	}
	if resp.Username != "alice" {
		t.Fatalf("Username = %q, want alice", resp.Username)
	}
	if resp.Email != "alice@example.com" {
		t.Fatalf("Email = %q", resp.Email)
	}
	if !resp.Active {
		t.Fatal("Active 应为 true")
	}
	if resp.CreatedAt != created.Unix() {
		t.Fatalf("CreatedAt = %d, want %d", resp.CreatedAt, created.Unix())
	}
}

func TestGetUser_NotFound(t *testing.T) {
	l := NewGetUserLogic(context.Background(), newTestSvc(newFakeUsersModel(), nil, nil))

	if _, err := l.GetUser(&user.GetUserRequest{UserId: 1}); err == nil {
		t.Fatal("用户不存在时应返回错误")
	}
}
