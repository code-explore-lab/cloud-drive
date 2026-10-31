package objkey

import (
	"strings"
	"testing"
)

func TestObjectKeepsOnlySafeExtension(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		want     string
	}{
		{"normal", "report.pdf", "u/7/upload-1.pdf"},
		{"no extension", "README", "u/7/upload-1"},
		{"upper case", "photo.JPG", "u/7/upload-1.JPG"},
		{"path traversal in name", "../../etc/passwd", "u/7/upload-1"},
		// path.Ext 只看最后一个元素，所以这里没有扩展名；
		// 关键点是斜杠不会出现在对象 Key 里。
		{"extension with slash", "evil.p/df", "u/7/upload-1"},
		{"extension with dot only", "evil.p.", "u/7/upload-1"},
		{"too long extension", "evil.abcdefghijklmnopq", "u/7/upload-1"},
		{"dot only", "evil.", "u/7/upload-1"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Object(7, "upload-1", c.fileName)
			if got != c.want {
				t.Fatalf("Object() = %q, want %q", got, c.want)
			}
			if strings.Contains(got, "..") {
				t.Fatalf("object key must not contain path traversal: %q", got)
			}
		})
	}
}

func TestPartKeyIsZeroPadded(t *testing.T) {
	if got := Part("abc", 7); got != "parts/abc/00007" {
		t.Fatalf("Part() = %q", got)
	}
	// 定长编号保证对象列表按字典序等于分片顺序。
	if Part("abc", 10) < Part("abc", 9) {
		t.Fatal("part keys must sort in part number order")
	}
}

func TestSafeExtStripsSeparators(t *testing.T) {
	for _, name := range []string{"a.tar.gz", "a.b/c", "a.b\\c"} {
		ext := SafeExt(name)
		if strings.ContainsAny(ext, "/\\") {
			t.Fatalf("SafeExt(%q) = %q contains a separator", name, ext)
		}
	}
}
