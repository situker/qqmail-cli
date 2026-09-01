package mimeparse

import (
	"mime"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestParseChineseMultipart(t *testing.T) {
	raw := strings.Join([]string{
		"From: =?UTF-8?B?5byg5LiJ?= <sender@example.com>",
		"To: receiver@qq.com",
		"Subject: =?UTF-8?B?5rWL6K+V6YKu5Lu2?=",
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="b"`,
		"", "--b", `Content-Type: text/plain; charset="UTF-8"`, "", "你好，世界",
		"--b", `Content-Type: application/octet-stream; name="report.txt"`, `Content-Disposition: attachment; filename="report.txt"`, "Content-Transfer-Encoding: base64", "", "Zml4dHVyZQ==", "--b--", "",
	}, "\r\n")
	got := Parse([]byte(raw))
	if got.Parser == "failed" || got.Subject != "测试邮件" || got.Text == nil || !strings.Contains(*got.Text, "你好") || len(got.From) != 1 || got.From[0].Name != "张三" || len(got.Attachments) != 1 || string(got.Attachments[0].Data) != "fixture" {
		t.Fatalf("unexpected parse: %#v", got)
	}
}

func TestParseQuotedEncodedDisplayName(t *testing.T) {
	raw := []byte("From: \"=?UTF-8?B?5byg5LiJ?=\" <sender@example.com>\r\nTo: receiver@qq.com\r\nSubject: fixture\r\n\r\nbody\r\n")
	got := Parse(raw)
	if len(got.From) != 1 || got.From[0].Name != "张三" {
		t.Fatalf("quoted encoded display name was not decoded: %#v", got.From)
	}
}

func TestSanitizeHTML(t *testing.T) {
	got := SanitizeHTML(`<p onclick="bad()">safe<script>alert(1)</script><img src="https://tracker/x"><a href="javascript:bad()">link</a></p>`)
	for _, forbidden := range []string{"script", "onclick", "tracker", "javascript:"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Fatalf("unsafe HTML remained: %s", got)
		}
	}
	if !strings.Contains(got, "safe") {
		t.Fatalf("safe content removed: %s", got)
	}
}

func TestSyntheticCorpusFortyMessages(t *testing.T) {
	type charsetCase struct {
		label string
		raw   []byte
	}
	chinese := "中文主题"
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(chinese))
	if err != nil {
		t.Fatal(err)
	}
	gb18030, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(chinese))
	if err != nil {
		t.Fatal(err)
	}
	charsets := []charsetCase{{"UTF-8", []byte(chinese)}, {"GBK", gbk}, {"GB2312", gbk}, {"GB18030", gb18030}}
	encoders := []mime.WordEncoder{mime.BEncoding, mime.QEncoding}
	count := 0
	for _, charset := range charsets {
		for _, encoder := range encoders {
			subject := encoder.Encode(charset.label, string(charset.raw))
			for variant := 0; variant < 5; variant++ {
				raw := syntheticMessage(subject, variant)
				got := Parse(raw)
				if got.Parser == "failed" || got.Subject != chinese || got.Text == nil {
					t.Fatalf("case %s/%T/%d failed: %#v", charset.label, encoder, variant, got)
				}
				assertVariantStructure(t, charset.label, variant, got)
				count++
			}
		}
	}
	if count != 40 {
		t.Fatalf("corpus size=%d, want 40", count)
	}
}

// assertVariantStructure makes each of the five MIME shapes actually visible
// to the assertions: body content, HTML presence, and attachment parsing per
// variant rather than only "parsed without failing".
func assertVariantStructure(t *testing.T, label string, variant int, got Result) {
	t.Helper()
	switch variant {
	case 0, 2, 3:
		if got.Text == nil || !strings.Contains(*got.Text, "fixture body") {
			t.Fatalf("case %s/%d: text body content missing: %#v", label, variant, got.Text)
		}
	}
	switch variant {
	case 1, 2, 4:
		if got.HTML == nil || !strings.Contains(*got.HTML, "fixture body") {
			t.Fatalf("case %s/%d: sanitized HTML missing: %#v", label, variant, got.HTML)
		}
		if strings.Contains(*got.HTML, "<script") {
			t.Fatalf("case %s/%d: HTML not sanitized", label, variant)
		}
	}
	if variant == 3 {
		if len(got.Attachments) != 1 || got.Attachments[0].Filename != "fixture.txt" || string(got.Attachments[0].Data) != "fixture" {
			t.Fatalf("case %s/%d: attachment parse wrong: %#v", label, variant, got.Attachments)
		}
	}
}

func syntheticMessage(subject string, variant int) []byte {
	headers := "From: sender@example.com\r\nTo: receiver@qq.com\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\n"
	var body string
	switch variant {
	case 0:
		body = "Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nfixture body\r\n"
	case 1:
		body = "Content-Type: text/html; charset=UTF-8\r\n\r\n<p>fixture body</p>\r\n"
	case 2:
		body = "Content-Type: multipart/alternative; boundary=a\r\n\r\n--a\r\nContent-Type: text/plain\r\n\r\nfixture body\r\n--a\r\nContent-Type: text/html\r\n\r\n<p>fixture body</p>\r\n--a--\r\n"
	case 3:
		body = "Content-Type: multipart/mixed; boundary=m\r\n\r\n--m\r\nContent-Type: text/plain\r\n\r\nfixture body\r\n--m\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=fixture.txt\r\nContent-Transfer-Encoding: base64\r\n\r\nZml4dHVyZQ==\r\n--m--\r\n"
	default:
		body = "Content-Type: multipart/related; boundary=r\r\n\r\n--r\r\nContent-Type: text/html\r\n\r\n<p>fixture body</p>\r\n--r\r\nContent-Type: image/png\r\nContent-Disposition: inline; filename=pixel.png\r\nContent-ID: <pixel>\r\nContent-Transfer-Encoding: base64\r\n\r\naQ==\r\n--r--\r\n"
	}
	return []byte(headers + body)
}
