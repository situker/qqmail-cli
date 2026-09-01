package safeio

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	for _, bad := range []string{"../../secret.txt", `..\\..\\secret.txt`, "CON", "\x1b[31m.txt", "..."} {
		got := SanitizeFilename(bad, "attachment-1")
		if got == "" || got == "." || got == ".." || strings.ContainsAny(got, "/\\\x1b") || strings.Contains(got, "..") || strings.EqualFold(got, "CON") {
			t.Errorf("unsafe result %q for %q", got, bad)
		}
	}
}

func TestUniquePathDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	first, err := UniquePath(dir, "报告.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFixture(first); err != nil {
		t.Fatal(err)
	}
	second, err := UniquePath(dir, "报告.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || filepath.Dir(second) != dir {
		t.Fatalf("unexpected unique path: %q, %q", first, second)
	}
}

func writeFixture(path string) error {
	return osWriteFile(path, []byte("x"), 0o600)
}
