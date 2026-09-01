package sendmail

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestBuildEncodesChineseHeadersAndAttachmentName(t *testing.T) {
	draft := Draft{
		From:    mail.Address{Name: "发件人", Address: "sender@qq.com"},
		To:      []mail.Address{{Name: "收件人", Address: "reader@example.com"}},
		Bcc:     []mail.Address{{Address: "hidden@example.com"}},
		Subject: "中文主题", Body: "你好，世界",
		InReplyTo: "original@example.com", References: []string{"older@example.com", "original@example.com"},
		Attachments: []Attachment{{Filename: "测试报告.pdf", ContentType: "application/pdf", Data: []byte("fixture")}},
	}
	raw, err := Build(draft)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(raw), []byte("bcc:")) || bytes.Contains(raw, []byte("hidden@example.com")) {
		t.Fatalf("Bcc leaked into MIME headers: %s", raw)
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil || subject != "中文主题" {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
	if message.Header.Get("In-Reply-To") != "<original@example.com>" || !strings.Contains(message.Header.Get("References"), "<older@example.com>") {
		t.Fatalf("thread headers missing: %#v", message.Header)
	}
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("content type=%q params=%v err=%v", mediaType, params, err)
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	_, err = reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	_, disposition, err := mime.ParseMediaType(attachment.Header.Get("Content-Disposition"))
	if err != nil || disposition["filename"] != "测试报告.pdf" {
		t.Fatalf("filename params=%v err=%v", disposition, err)
	}
	data, err := io.ReadAll(attachment)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		t.Fatalf("attachment body missing: %q err=%v", data, err)
	}
}

func TestRecipientAllowlistRequiresEveryRecipient(t *testing.T) {
	draft := Draft{To: []mail.Address{{Address: "one@example.com"}}, Cc: []mail.Address{{Address: "two@trusted.test"}}}
	if denied := DeniedRecipients(draft, []string{"one@example.com", "*@trusted.test"}); len(denied) != 0 {
		t.Fatalf("unexpected denied recipients: %v", denied)
	}
	if denied := DeniedRecipients(draft, nil); len(denied) != 2 {
		t.Fatalf("empty allowlist did not deny all recipients: %v", denied)
	}
	// Partial hit: one allowed recipient must not carry a stranger through,
	// and the denial must name exactly the stranger.
	partial := Draft{To: []mail.Address{{Address: "one@example.com"}}, Cc: []mail.Address{{Address: "stranger@evil.test"}}}
	denied := DeniedRecipients(partial, []string{"one@example.com"})
	if len(denied) != 1 || denied[0] != "stranger@evil.test" {
		t.Fatalf("partial allowlist hit mishandled: %v", denied)
	}
	// Bcc participates in the check like every other recipient.
	hidden := Draft{To: []mail.Address{{Address: "one@example.com"}}, Bcc: []mail.Address{{Address: "sneak@evil.test"}}}
	denied = DeniedRecipients(hidden, []string{"one@example.com"})
	if len(denied) != 1 || denied[0] != "sneak@evil.test" {
		t.Fatalf("bcc escaped the allowlist: %v", denied)
	}
}

func TestHeaderInjectionRejected(t *testing.T) {
	_, err := Build(Draft{From: mail.Address{Address: "a@example.com"}, To: []mail.Address{{Address: "b@example.com"}}, Subject: "ok\r\nBcc: bad@example.com"})
	if err == nil {
		t.Fatal("expected header injection rejection")
	}
}
