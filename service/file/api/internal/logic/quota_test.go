package logic

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNormalizeRequestId(t *testing.T) {
	if got, err := normalizeRequestId(""); err != nil || got != "" {
		t.Fatalf("empty key should stay empty, got %q err %v", got, err)
	}

	valid := "3f1b2c4d-aaaa-bbbb-cccc-1234567890ab"
	got, err := normalizeRequestId("  " + valid + "  ")
	if err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if got != valid {
		t.Fatalf("key should be trimmed, got %q", got)
	}

	invalid := []string{"short", "with space", "中文中文中文中文", "a/b/c/d/e", string(make([]byte, 80))}
	for _, key := range invalid {
		if _, err := normalizeRequestId(key); err == nil {
			t.Fatalf("key %q should be rejected", key)
		} else if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("key %q should produce InvalidArgument, got %v", key, status.Code(err))
		}
	}
}
