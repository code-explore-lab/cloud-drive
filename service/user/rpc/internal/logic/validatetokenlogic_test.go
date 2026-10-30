package logic

import (
	"context"
	"testing"
	"time"

	"cloud-drive/common/jwtx"
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/types/user"
)

func TestValidateToken_Success(t *testing.T) {
	token, err := jwtx.Generate(testJWTSecret, 1, "alice", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	users := newFakeUsersModel()
	users.findOne = func(_ context.Context, id uint64) (*model.Users, error) {
		return &model.Users{Id: id, Username: "alice", Status: 1}, nil
	}

	l := NewValidateTokenLogic(context.Background(), newTestSvc(users, nil, nil))
	resp, err := l.ValidateToken(&user.ValidateTokenRequest{AccessToken: token})
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if !resp.Valid {
		t.Fatal("token 应当有效")
	}
	if resp.UserId != 1 || resp.Username != "alice" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestValidateToken_InvalidToken(t *testing.T) {
	l := NewValidateTokenLogic(context.Background(), newTestSvc(newFakeUsersModel(), nil, nil))

	resp, err := l.ValidateToken(&user.ValidateTokenRequest{AccessToken: "not-a-jwt"})
	if err != nil {
		t.Fatalf("token 无效不应返回 gRPC 错误: %v", err)
	}
	if resp.Valid {
		t.Fatal("非法 token 不应有效")
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	token, err := jwtx.Generate(testJWTSecret, 1, "alice", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	l := NewValidateTokenLogic(context.Background(), newTestSvc(newFakeUsersModel(), nil, nil))
	resp, err := l.ValidateToken(&user.ValidateTokenRequest{AccessToken: token})
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if resp.Valid {
		t.Fatal("过期 token 不应有效")
	}
}

func TestValidateToken_DisabledUser(t *testing.T) {
	token, err := jwtx.Generate(testJWTSecret, 1, "alice", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	users := newFakeUsersModel()
	users.findOne = func(_ context.Context, id uint64) (*model.Users, error) {
		return &model.Users{Id: id, Username: "alice", Status: 0}, nil
	}

	l := NewValidateTokenLogic(context.Background(), newTestSvc(users, nil, nil))
	resp, err := l.ValidateToken(&user.ValidateTokenRequest{AccessToken: token})
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if resp.Valid {
		t.Fatal("被禁用用户的 token 不应有效")
	}
}
