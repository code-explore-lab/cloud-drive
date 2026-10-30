package logic

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"cloud-drive/common/jwtx"
	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/types/user"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newRefreshFixture 构造一个「旧 token 仍然有效」的场景。
func newRefreshFixture(t *testing.T, plain string) (*fakeTokensModel, *fakeUsersModel, *model.RefreshTokens, **model.RefreshTokens, **model.RefreshTokens) {
	t.Helper()

	old := &model.RefreshTokens{
		Id:        10,
		UserId:    1,
		TokenHash: hashToken(plain),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	tokens := newFakeTokensModel()
	tokens.findByTokenHash = func(_ context.Context, tokenHash string) (*model.RefreshTokens, error) {
		if tokenHash != old.TokenHash {
			return nil, model.ErrNotFound
		}
		return old, nil
	}

	var inserted *model.RefreshTokens
	tokens.insert = func(_ context.Context, data *model.RefreshTokens) (sql.Result, error) {
		inserted = data
		return fakeResult{id: 11, rows: 1}, nil
	}
	var updated *model.RefreshTokens
	tokens.update = func(_ context.Context, data *model.RefreshTokens) error {
		updated = data
		return nil
	}

	users := newFakeUsersModel()
	users.findOne = func(_ context.Context, id uint64) (*model.Users, error) {
		return &model.Users{Id: id, Username: "alice", Status: 1}, nil
	}

	return tokens, users, old, &inserted, &updated
}

func TestRefreshToken_Success(t *testing.T) {
	const plain = "old-refresh-token"
	tokens, users, _, inserted, updated := newRefreshFixture(t, plain)

	l := NewRefreshTokenLogic(context.Background(), newTestSvc(users, nil, tokens))
	resp, err := l.RefreshToken(&user.RefreshTokenRequest{RefreshToken: plain})
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}

	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("新 token 不应为空")
	}
	claims, err := jwtx.Parse(testJWTSecret, resp.AccessToken)
	if err != nil {
		t.Fatalf("解析 access token: %v", err)
	}
	if claims.UserId != 1 || claims.Username != "alice" {
		t.Fatalf("claims = %+v", claims)
	}

	if *inserted == nil {
		t.Fatal("新的 refresh token 应当被写入")
	}
	if (*inserted).TokenHash != hashToken(resp.RefreshToken) {
		t.Fatal("新 refresh token 应存哈希")
	}
	if *updated == nil {
		t.Fatal("旧的 refresh token 应当被作废")
	}
	if !(*updated).RevokedAt.Valid {
		t.Fatal("旧 refresh token 的 RevokedAt 应被设置")
	}
}

func TestRefreshToken_NotFound(t *testing.T) {
	l := NewRefreshTokenLogic(context.Background(), newTestSvc(newFakeUsersModel(), nil, newFakeTokensModel()))
	_, err := l.RefreshToken(&user.RefreshTokenRequest{RefreshToken: "unknown"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestRefreshToken_Revoked(t *testing.T) {
	const plain = "old-refresh-token"
	tokens, users, old, _, _ := newRefreshFixture(t, plain)
	old.RevokedAt = sql.NullTime{Time: time.Now(), Valid: true}

	l := NewRefreshTokenLogic(context.Background(), newTestSvc(users, nil, tokens))
	_, err := l.RefreshToken(&user.RefreshTokenRequest{RefreshToken: plain})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestRefreshToken_Expired(t *testing.T) {
	const plain = "old-refresh-token"
	tokens, users, old, _, _ := newRefreshFixture(t, plain)
	old.ExpiresAt = time.Now().Add(-time.Minute)

	l := NewRefreshTokenLogic(context.Background(), newTestSvc(users, nil, tokens))
	_, err := l.RefreshToken(&user.RefreshTokenRequest{RefreshToken: plain})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestRefreshToken_DisabledUser(t *testing.T) {
	const plain = "old-refresh-token"
	tokens, users, _, _, _ := newRefreshFixture(t, plain)
	users.findOne = func(_ context.Context, id uint64) (*model.Users, error) {
		return &model.Users{Id: id, Username: "alice", Status: 0}, nil
	}

	l := NewRefreshTokenLogic(context.Background(), newTestSvc(users, nil, tokens))
	_, err := l.RefreshToken(&user.RefreshTokenRequest{RefreshToken: plain})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", status.Code(err))
	}
}
