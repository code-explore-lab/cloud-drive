package logic

import (
	"strings"
	"testing"

	"cloud-drive/service/storage/model"
)

func TestExpectedPartSize(t *testing.T) {
	task := &model.UploadTasks{TotalSize: 10, ChunkSize: 4, TotalChunks: 3}
	cases := map[int32]int64{1: 4, 2: 4, 3: 2}
	for part, want := range cases {
		if got := expectedPartSize(task, part); got != want {
			t.Fatalf("part %d: got %d want %d", part, got, want)
		}
	}
}

func TestExpectedPartSizeForEmptyFile(t *testing.T) {
	task := &model.UploadTasks{TotalSize: 0, ChunkSize: 8, TotalChunks: 1}
	if got := expectedPartSize(task, 1); got != 0 {
		t.Fatalf("empty file part size = %d, want 0", got)
	}
}

func makePart(number uint64, size uint64) *model.UploadParts {
	return &model.UploadParts{
		UploadId:     "u1",
		PartNumber:   number,
		Size:         size,
		ObjectPartId: partObjectKey("u1", int32(number)),
	}
}

func TestValidateParts(t *testing.T) {
	task := &model.UploadTasks{UploadId: "u1", TotalSize: 10, ChunkSize: 4, TotalChunks: 3}

	ok := []*model.UploadParts{makePart(1, 4), makePart(2, 4), makePart(3, 2)}
	if err := validateParts(task, ok); err != nil {
		t.Fatalf("complete parts should validate: %v", err)
	}

	incomplete := []*model.UploadParts{makePart(1, 4), makePart(3, 2)}
	if err := validateParts(task, incomplete); err == nil {
		t.Fatal("missing part must be rejected")
	}

	wrongSize := []*model.UploadParts{makePart(1, 4), makePart(2, 1), makePart(3, 2)}
	if err := validateParts(task, wrongSize); err == nil {
		t.Fatal("wrong part size must be rejected")
	}

	noObject := []*model.UploadParts{makePart(1, 4), makePart(2, 4), makePart(3, 2)}
	noObject[1].ObjectPartId = ""
	if err := validateParts(task, noObject); err == nil {
		t.Fatal("part without object must be rejected")
	}
}

func TestObjectKeyForUsesUserNamespace(t *testing.T) {
	key := objectKeyFor(42, "upload-id", "report.pdf")
	if key != "u/42/upload-id.pdf" {
		t.Fatalf("object key = %q", key)
	}
	if strings.Contains(key, "..") {
		t.Fatal("object key must not allow traversal")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("truncate short string = %q", got)
	}
}
