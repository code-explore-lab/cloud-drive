package handler

import (
	"strings"
	"testing"
)

func TestParseRange(t *testing.T) {
	const size int64 = 1000

	cases := []struct {
		name       string
		header     string
		wantStart  int64
		wantLength int64
		wantRanged bool
		wantErr    bool
	}{
		{"empty header", "", 0, size, false, false},
		{"first bytes", "bytes=0-99", 0, 100, true, false},
		{"middle", "bytes=200-299", 200, 100, true, false},
		{"open ended", "bytes=900-", 900, 100, true, false},
		{"suffix", "bytes=-100", 900, 100, true, false},
		{"suffix larger than object", "bytes=-5000", 0, size, true, false},
		{"clamped end", "bytes=0-5000", 0, size, false, false},
		{"whole object", "bytes=0-999", 0, size, false, false},
		{"bad unit", "items=0-1", 0, 0, false, true},
		{"multiple ranges", "bytes=0-1,5-6", 0, 0, false, true},
		{"start beyond size", "bytes=1000-", 0, 0, false, true},
		{"end before start", "bytes=100-50", 0, 0, false, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, length, ranged, err := parseRange(c.header, size)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if start != c.wantStart || length != c.wantLength || ranged != c.wantRanged {
				t.Fatalf("got start=%d length=%d ranged=%v, want %d/%d/%v",
					start, length, ranged, c.wantStart, c.wantLength, c.wantRanged)
			}
		})
	}
}

func TestParseRangeOfEmptyObject(t *testing.T) {
	// 空对象上的任何 Range 都无法满足，应返回 416 的判定结果。
	if _, _, _, err := parseRange("bytes=0-", 0); err == nil {
		t.Fatal("range on empty object should fail")
	}
}

func TestContentDispositionRemovesDangerousCharacters(t *testing.T) {
	got := contentDisposition("a\"b\\c.txt")
	if strings.Contains(got, `"a"`) || strings.Contains(got, `\c`) {
		t.Fatalf("header %q still contains control characters", got)
	}
	if !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("header %q should be an attachment", got)
	}
	if contentDisposition("") != "attachment" {
		t.Fatal("empty filename should produce a bare attachment header")
	}
}

func TestExpectedPartSize(t *testing.T) {
	const chunk = 8 << 20
	cases := []struct {
		name        string
		totalSize   int64
		totalChunks int64
		part        int64
		want        int64
	}{
		{"single small part", 100, 1, 1, 100},
		{"exact multiple first part", chunk * 2, 2, 1, chunk},
		{"exact multiple last part", chunk * 2, 2, 2, chunk},
		{"ragged last part", chunk + 1, 2, 2, 1},
		{"empty file", 0, 1, 1, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expectedPartSize(chunk, c.totalChunks, c.totalSize, c.part)
			if got != c.want {
				t.Fatalf("expectedPartSize() = %d, want %d", got, c.want)
			}
		})
	}
}
