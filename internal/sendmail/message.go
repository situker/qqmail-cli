package sendmail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/situker/qqmailctl/internal/safeio"
)

const (
	MaxTotalAttachmentBytes int64 = 20 << 20
	MaxRecipientsPerMessage       = 10
)

type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}

type Draft struct {
	From        mail.Address
	To          []mail.Address
	Cc          []mail.Address
	Bcc         []mail.Address
	Subject     string
	Body        string
	Attachments []Attachment
	InReplyTo   string
	References  []string
	Date        time.Time
}

type AttachmentSummary struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int    `json:"size_bytes"`
}

type Summary struct {
	From             string              `json:"from"`
	To               []string            `json:"to"`
	Cc               []string            `json:"cc"`
	Bcc              []string            `json:"bcc"`
	Subject          string              `json:"subject"`
	BodySummary      string              `json:"body_summary"`
	Attachments      []AttachmentSummary `json:"attachments"`
	RecipientCount   int                 `json:"recipient_count"`
	AllowlistReady   bool                `json:"allowlist_ready"`
	DeniedRecipients []string            `json:"denied_recipients"`
}

func Build(draft Draft) ([]byte, error) {
	if err := validateDraft(draft); err != nil {
		return nil, err
	}
	if draft.Date.IsZero() {
		draft.Date = time.Now()
	}
	messageID, err := newMessageID(draft.From.Address)
	if err != nil {
		return nil, err
	}
	headers := []header{
		{"Date", draft.Date.Format(time.RFC1123Z)},
		{"Message-ID", messageID},
		{"From", draft.From.String()},
		{"To", addressList(draft.To)},
		{"Subject", mime.QEncoding.Encode("UTF-8", draft.Subject)},
		{"MIME-Version", "1.0"},
	}
	if len(draft.Cc) > 0 {
		headers = append(headers, header{"Cc", addressList(draft.Cc)})
	}
	if draft.InReplyTo != "" {
		headers = append(headers, header{"In-Reply-To", formatMessageID(draft.InReplyTo)})
	}
	if len(draft.References) > 0 {
		values := make([]string, 0, len(draft.References))
		for _, value := range draft.References {
			if strings.TrimSpace(value) != "" {
				values = append(values, formatMessageID(value))
			}
		}
		if len(values) > 0 {
			headers = append(headers, header{"References", strings.Join(values, " ")})
		}
	}
	var output bytes.Buffer
	if len(draft.Attachments) == 0 {
		headers = append(headers, header{"Content-Type", `text/plain; charset="UTF-8"`}, header{"Content-Transfer-Encoding", "quoted-printable"})
		writeHeaders(&output, headers)
		writer := quotedprintable.NewWriter(&output)
		_, _ = writer.Write([]byte(normalizeCRLF(draft.Body)))
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return output.Bytes(), nil
	}
	multipartWriter := multipart.NewWriter(&output)
	headers = append(headers, header{"Content-Type", fmt.Sprintf(`multipart/mixed; boundary="%s"`, multipartWriter.Boundary())})
	writeHeaders(&output, headers)
	textHeader := textproto.MIMEHeader{}
	textHeader.Set("Content-Type", `text/plain; charset="UTF-8"`)
	textHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := multipartWriter.CreatePart(textHeader)
	if err != nil {
		return nil, err
	}
	quoted := quotedprintable.NewWriter(part)
	_, _ = quoted.Write([]byte(normalizeCRLF(draft.Body)))
	if err := quoted.Close(); err != nil {
		return nil, err
	}
	for i, attachment := range draft.Attachments {
		filename := safeio.SanitizeFilename(attachment.Filename, fmt.Sprintf("attachment-%d", i+1))
		contentType := attachment.ContentType
		if contentType == "" {
			contentType = mime.TypeByExtension(filepath.Ext(filename))
		}
		contentType = safeContentType(contentType)
		partHeader := textproto.MIMEHeader{}
		partHeader.Set("Content-Type", mime.FormatMediaType(contentType, map[string]string{"name": filename}))
		partHeader.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
		partHeader.Set("Content-Transfer-Encoding", "base64")
		part, err := multipartWriter.CreatePart(partHeader)
		if err != nil {
			return nil, err
		}
		encoded := base64.StdEncoding.EncodeToString(attachment.Data)
		for len(encoded) > 76 {
			_, _ = fmt.Fprintf(part, "%s\r\n", encoded[:76])
			encoded = encoded[76:]
		}
		if encoded != "" {
			_, _ = fmt.Fprintf(part, "%s\r\n", encoded)
		}
	}
	if err := multipartWriter.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func Summarize(draft Draft, allowlist []string) Summary {
	denied := DeniedRecipients(draft, allowlist)
	summary := Summary{From: draft.From.String(), To: addressStrings(draft.To), Cc: addressStrings(draft.Cc), Bcc: addressStrings(draft.Bcc), Subject: draft.Subject, BodySummary: truncateRunes(strings.TrimSpace(draft.Body), 240), Attachments: []AttachmentSummary{}, RecipientCount: len(draft.To) + len(draft.Cc) + len(draft.Bcc), AllowlistReady: len(allowlist) > 0 && len(denied) == 0, DeniedRecipients: denied}
	for i, attachment := range draft.Attachments {
		filename := safeio.SanitizeFilename(attachment.Filename, fmt.Sprintf("attachment-%d", i+1))
		summary.Attachments = append(summary.Attachments, AttachmentSummary{Filename: filename, ContentType: attachment.ContentType, SizeBytes: len(attachment.Data)})
	}
	return summary
}

func DeniedRecipients(draft Draft, allowlist []string) []string {
	denied := []string{}
	for _, address := range append(append(append([]mail.Address{}, draft.To...), draft.Cc...), draft.Bcc...) {
		if !allowed(address.Address, allowlist) {
			denied = append(denied, strings.ToLower(address.Address))
		}
	}
	return denied
}

func Recipients(draft Draft) []string {
	result := []string{}
	for _, address := range append(append(append([]mail.Address{}, draft.To...), draft.Cc...), draft.Bcc...) {
		result = append(result, address.Address)
	}
	return result
}

type header struct{ name, value string }

func validateDraft(draft Draft) error {
	if strings.TrimSpace(draft.From.Address) == "" || len(draft.To)+len(draft.Cc)+len(draft.Bcc) == 0 {
		return fmt.Errorf("from and at least one recipient are required")
	}
	if len(draft.To)+len(draft.Cc)+len(draft.Bcc) > MaxRecipientsPerMessage {
		return fmt.Errorf("recipient count exceeds %d", MaxRecipientsPerMessage)
	}
	for _, value := range []string{draft.Subject, draft.InReplyTo, strings.Join(draft.References, " "), draft.From.Name, draft.From.Address} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("header contains a line break")
		}
	}
	for _, address := range append(append(append([]mail.Address{}, draft.To...), draft.Cc...), draft.Bcc...) {
		if strings.ContainsAny(address.Name+address.Address, "\r\n") {
			return fmt.Errorf("address contains a line break")
		}
	}
	var total int64
	for _, attachment := range draft.Attachments {
		total += int64(len(attachment.Data))
	}
	if total > MaxTotalAttachmentBytes {
		return fmt.Errorf("attachments exceed %d bytes", MaxTotalAttachmentBytes)
	}
	return nil
}

func safeContentType(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || !strings.Contains(mediaType, "/") || strings.ContainsAny(mediaType, "\r\n") {
		return "application/octet-stream"
	}
	return mediaType
}

func writeHeaders(output *bytes.Buffer, headers []header) {
	for _, header := range headers {
		if header.value != "" {
			_, _ = fmt.Fprintf(output, "%s: %s\r\n", header.name, header.value)
		}
	}
	output.WriteString("\r\n")
}

func addressList(values []mail.Address) string { return strings.Join(addressStrings(values), ", ") }
func addressStrings(values []mail.Address) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = values[i].String()
	}
	return result
}

func allowed(address string, allowlist []string) bool {
	address = strings.ToLower(strings.TrimSpace(address))
	for _, entry := range allowlist {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == address {
			return true
		}
		if strings.HasPrefix(entry, "*@") && strings.HasSuffix(address, entry[1:]) && strings.Count(address, "@") == 1 {
			return true
		}
	}
	return false
}

func newMessageID(address string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	domain := "localhost"
	if at := strings.LastIndexByte(address, '@'); at >= 0 && at < len(address)-1 {
		domain = address[at+1:]
	}
	return fmt.Sprintf("<%x@%s>", raw, domain), nil
}

func formatMessageID(value string) string {
	return "<" + strings.Trim(strings.TrimSpace(value), "<>") + ">"
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func truncateRunes(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max]) + "…"
}
