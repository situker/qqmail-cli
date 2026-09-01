package output

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestRedactString(t *testing.T) {
	secret := "abcdefghijklmnop"
	RegisterSecret(secret)
	got := RedactString("plain=" + secret + " base64=" + base64.StdEncoding.EncodeToString([]byte(secret)))
	if strings.Contains(got, secret) || strings.Contains(got, base64.StdEncoding.EncodeToString([]byte(secret))) {
		t.Fatalf("secret leaked: %s", got)
	}
}

func TestSanitizeHuman(t *testing.T) {
	got := SanitizeHuman("safe\x1b[31mred\x1b[0m\u202eevil\x00")
	if strings.ContainsAny(got, "\x1b\x00\u202e") {
		t.Fatalf("controls not removed: %q", got)
	}
}

func TestSanitizeMarkdown(t *testing.T) {
	got := SanitizeMarkdown("[click](x)|ok\nnext\x1b[31m")
	if strings.ContainsAny(got, "[]()|\n\x1b") {
		t.Fatalf("Markdown syntax survived sanitization: %q", got)
	}
}
