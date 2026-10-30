package jwtx

import (
	"testing"
	"time"
)

func TestGenerateAndParse(t *testing.T) {
	const secret = "test-secret"
	exp := time.Now().Add(time.Minute)

	token, err := Generate(secret, 42, "alice", exp)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if token == "" {
		t.Fatal("Generate 返回了空 token")
	}

	claims, err := Parse(secret, token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserId != 42 {
		t.Fatalf("UserId = %d, want 42", claims.UserId)
	}
	if claims.Username != "alice" {
		t.Fatalf("Username = %q, want alice", claims.Username)
	}
}

func TestParse_WrongSecret(t *testing.T) {
	token, err := Generate("secret-a", 1, "alice", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, err := Parse("secret-b", token); err == nil {
		t.Fatal("使用错误的 secret 解析应当失败")
	}
}

func TestParse_Expired(t *testing.T) {
	const secret = "test-secret"
	token, err := Generate(secret, 1, "alice", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, err := Parse(secret, token); err == nil {
		t.Fatal("过期 token 解析应当失败")
	}
}

func TestParse_Malformed(t *testing.T) {
	if _, err := Parse("secret", "not-a-jwt"); err == nil {
		t.Fatal("非法 token 解析应当失败")
	}
}
