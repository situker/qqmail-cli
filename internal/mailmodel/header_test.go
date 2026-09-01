package mailmodel

import "testing"

func TestDecodeHeaderText(t *testing.T) {
	const encoded = "=?UTF-8?B?5byg5LiJ?="
	if got := DecodeHeaderText(encoded); got != "张三" {
		t.Fatalf("DecodeHeaderText(%q) = %q, want 张三", encoded, got)
	}

	const malformed = "=?unknown?B?5byg5LiJ?="
	if got := DecodeHeaderText(malformed); got != malformed {
		t.Fatalf("unknown charset should be preserved, got %q", got)
	}
}
