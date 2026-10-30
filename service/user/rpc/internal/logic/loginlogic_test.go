package logic

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"cloud-drive/common/cryptx"
	"cloud-drive/common/jwtx"
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/types/user"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestLogin_Success(t *testing.T) {
	users := newFakeUsersModel()
	users.findOneByUsername = func(_ context.Context, username string) (*model.Users, error) {
		return &model.Users{
			Id:           1,
			Username:     username,
			Status:       1,
			PasswordHash: cryptx.PasswordEncrypt(testSalt, "password123"),
		}, nil
	}

	var stored *model.RefreshTokens
	tokens := newFakeTokensModel()
	tokens.insert = func(_ context.Context, data *model.RefreshTokens) (sql.Result, error) {
		stored = data
		return fakeResult{id: 1, rows: 1}, nil
	}

	l := NewLoginLogic(context.Background(), newTestSvc(users, nil, tokens))
	resp, err := l.Login(&user.LoginRequest{Username: "alice", Password: "password123"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("access token 和 refresh token 都不应为空")
	}
	claims, err := jwtx.Parse(testJWTSecret, resp.AccessToken)
	if err != nil {
		t.Fatalf("解析签发的 access token: %v", err)
	}
	if claims.UserId != 1 || claims.Username != "alice" {
		t.Fatalf("claims = %+v", claims)
	}

	if stored == nil {
		t.Fatal("refresh token 应当被写入")
	}
	if stored.UserId != 1 {
		t.Fatalf("stored.UserId = %d, want 1", stored.UserId)
	}
	if stored.TokenHash != hashToken(resp.RefreshToken) {
		t.Fatal("数据库里应存 refresh token 的哈希，而不是明文")
	}
	if !stored.ExpiresAt.After(time.Now()) {
		t.Fatal("refresh token 的过期时间应在未来")
	}
	if resp.ExpiresAt <= time.Now().Unix() {
		t.Fatal("access token 的过期时间应在未来")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	users := newFakeUsersModel()
	users.findOneByUsername = func(_ context.Context, username string) (*model.Users, error) {
		return &model.Users{
			Id:           1,
			Username:     username,
			Status:       1,
			PasswordHash: cryptx.PasswordEncrypt(testSalt, "correct-password"),
		}, nil
	}

	l := NewLoginLogic(context.Background(), newTestSvc(users, nil, newFakeTokensModel()))
	_, err := l.Login(&user.LoginRequest{Username: "alice", Password: "wrong-password"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	l := NewLoginLogic(context.Background(), newTestSvc(newFakeUsersModel(), nil, newFakeTokensModel()))
	_, err := l.Login(&user.LoginRequest{Username: "nobody", Password: "password123"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestLogin_DisabledUser(t *testing.T) {
	users := newFakeUsersModel()
	users.findOneByUsername = func(_ context.Context, username string) (*model.Users, error) {
		return &model.Users{
			Id:           1,
			Username:     username,
			Status:       0, // 禁用
			PasswordHash: cryptx.PasswordEncrypt(testSalt, "password123"),
		}, nil
	}

	l := NewLoginLogic(context.Background(), newTestSvc(users, nil, newFakeTokensModel()))
	_, err := l.Login(&user.LoginRequest{Username: "alice", Password: "password123"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", status.Code(err))
	}
}
