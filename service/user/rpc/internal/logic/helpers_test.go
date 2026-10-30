package logic

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestHashToken(t *testing.T) {
	got := hashToken("abc")

	sum := sha256.Sum256([]byte("abc"))
	want := hex.EncodeToString(sum[:])

	if got != want {
		t.Fatalf("hashToken = %q, want %q", got, want)
	}
	if got != hashToken("abc") {
		t.Fatal("hashToken 对同一输入必须返回相同结果")
	}
	if got == hashToken("abd") {
		t.Fatal("不同输入应得到不同哈希")
	}
	if len(got) != 64 {
		t.Fatalf("sha256 hex 长度应为 64，实际 %d", len(got))
	}
}

func TestRandomToken(t *testing.T) {
	a, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}
	b, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}

	if len(a) != 64 {
		t.Fatalf("32 字节转 hex 后长度应为 64，实际 %d", len(a))
	}
	if a == b {
		t.Fatal("两次生成的 token 不应相同")
	}
}
