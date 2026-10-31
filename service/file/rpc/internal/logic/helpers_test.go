package logic

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{"a.txt", "报告.pdf", "a b", strings.Repeat("x", 255)}
	for _, name := range valid {
		if _, err := validateName(name); err != nil {
			t.Fatalf("validateName(%q) should pass: %v", name, err)
		}
	}

	invalid := []string{"", "   ", ".", "..", "a/b", "a\\b", "a\x00b", "a\nb",
		strings.Repeat("x", 256)}
	for _, name := range invalid {
		if _, err := validateName(name); err == nil {
			t.Fatalf("validateName(%q) should fail", name)
		}
	}
}

func TestValidateNameTrimsSurroundingSpaces(t *testing.T) {
	got, err := validateName("  report.txt  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "report.txt" {
		t.Fatalf("trimmed name = %q", got)
	}
}

func TestShareTokenIsUnpredictableAndHashed(t *testing.T) {
	tokens := make(map[string]struct{}, 32)
	for i := 0; i < 32; i++ {
		token, err := newShareToken()
		if err != nil {
			t.Fatalf("newShareToken: %v", err)
		}
		if len(token) < 40 {
			t.Fatalf("token %q is too short", token)
		}
		if _, dup := tokens[token]; dup {
			t.Fatal("share tokens must not repeat")
		}
		tokens[token] = struct{}{}

		hash := hashShareToken(token)
		if len(hash) != 64 {
			t.Fatalf("token hash must be sha256 hex, got %q", hash)
		}
		if hash == token {
			t.Fatal("token must not be stored in plaintext")
		}
	}
}

func TestSharePasswordHashAndVerify(t *testing.T) {
	hash, err := hashSharePassword("s3cret-pass")
	if err != nil {
		t.Fatalf("hashSharePassword: %v", err)
	}
	if hash == "" || hash == "s3cret-pass" {
		t.Fatal("password must be hashed")
	}
	if !verifySharePassword(hash, "s3cret-pass") {
		t.Fatal("correct password should verify")
	}
	if verifySharePassword(hash, "wrong") {
		t.Fatal("wrong password must not verify")
	}
	if !verifySharePassword("", "anything") {
		t.Fatal("empty hash means no password required")
	}
}

func TestHashSharePasswordRejectsTooLong(t *testing.T) {
	if _, err := hashSharePassword(strings.Repeat("a", 80)); err == nil {
		t.Fatal("bcrypt input limit must be enforced")
	}
}
