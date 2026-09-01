package mimeparse

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset"
	messagemail "github.com/emersion/go-message/mail"
	enmime "github.com/jhillyerd/enmime/v2"
	"github.com/microcosm-cc/bluemonday"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"golang.org/x/net/html"
)

const MaxPartBytes int64 = 64 << 20

type Result struct {
	Subject     string
	From        []mailmodel.Address
	To          []mailmodel.Address
	Date        time.Time
	MessageID   string
	Text        *string
	HTML        *string
	Attachments []mailmodel.Attachment
	Parser      string
	Warnings    []string
}

func Parse(raw []byte) Result {
	primary, err := parseGoMessage(raw)
	if err == nil {
		primary.Parser = "go-message"
		return primary
	}
	fallback, fallbackErr := parseEnmime(raw)
	if fallbackErr == nil {
		fallback.Parser = "enmime"
		fallback.Warnings = append(fallback.Warnings, "主 MIME 解析器失败，已使用兼容解析器")
		return fallback
	}
	return Result{Parser: "failed", Warnings: []string{fmt.Sprintf("MIME 解析失败：%v", errors.Join(err, fallbackErr))}}
}

func parseGoMessage(raw []byte) (Result, error) {
	reader, err := messagemail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = reader.Close() }()
	result := Result{Subject: textHeader(&reader.Header, "Subject")}
	result.From = messageAddresses(&reader.Header, "From")
	result.To = messageAddresses(&reader.Header, "To")
	result.Date, _ = reader.Header.Date()
	result.MessageID, _ = reader.Header.MessageID()
	attachmentIndex := 1
	for parts := 0; ; parts++ {
		if parts >= 1000 {
			return Result{}, fmt.Errorf("MIME part count exceeds 1000")
		}
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Result{}, err
		}
		content, err := readLimited(part.Body, MaxPartBytes)
		if err != nil {
			return Result{}, err
		}
		switch header := part.Header.(type) {
		case *messagemail.InlineHeader:
			mediaType, _, _ := header.ContentType()
			switch strings.ToLower(mediaType) {
			case "text/plain":
				if result.Text == nil {
					value := string(content)
					result.Text = &value
				}
			case "text/html":
				if result.HTML == nil {
					value := SanitizeHTML(string(content))
					result.HTML = &value
				}
			}
		case *messagemail.AttachmentHeader:
			filename, _ := header.Filename()
			mediaType, _, _ := header.ContentType()
			result.Attachments = append(result.Attachments, mailmodel.Attachment{Index: attachmentIndex, Filename: filename, ContentType: mediaType, Size: int64(len(content)), Data: content})
			attachmentIndex++
		default:
			return Result{}, fmt.Errorf("unknown MIME part type %T", part.Header)
		}
	}
	fillTextFallback(&result)
	return result, nil
}

func parseEnmime(raw []byte) (Result, error) {
	envelope, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	result := Result{Subject: envelope.GetHeader("Subject"), MessageID: strings.Trim(envelope.GetHeader("Message-ID"), "<> ")}
	result.From = standardAddresses(envelope.AddressList("From"))
	result.To = standardAddresses(envelope.AddressList("To"))
	result.Date, _ = envelope.Date()
	if envelope.Text != "" {
		value := envelope.Text
		result.Text = &value
	}
	if envelope.HTML != "" {
		value := SanitizeHTML(envelope.HTML)
		result.HTML = &value
	}
	parts := append(append([]*enmime.Part{}, envelope.Attachments...), envelope.Inlines...)
	for i, part := range parts {
		if part.FileName == "" && !strings.EqualFold(part.Disposition, "attachment") {
			continue
		}
		result.Attachments = append(result.Attachments, mailmodel.Attachment{Index: i + 1, Filename: part.FileName, ContentType: part.ContentType, Size: int64(len(part.Content)), ContentID: part.ContentID, Data: part.Content})
	}
	fillTextFallback(&result)
	return result, nil
}

func SanitizeHTML(value string) string {
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "br", "div", "span", "strong", "em", "b", "i", "u", "ul", "ol", "li", "blockquote", "pre", "code", "table", "thead", "tbody", "tr", "th", "td", "hr", "h1", "h2", "h3", "h4", "h5", "h6", "a")
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowStandardURLs()
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)
	return policy.Sanitize(value)
}

func HTMLToText(value string) string {
	node, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			text := strings.TrimSpace(current.Data)
			if text != "" {
				if builder.Len() > 0 {
					builder.WriteByte(' ')
				}
				builder.WriteString(text)
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func fillTextFallback(result *Result) {
	if result.Text == nil && result.HTML != nil {
		value := HTMLToText(*result.HTML)
		result.Text = &value
	}
}

func textHeader(header *messagemail.Header, key string) string {
	value, err := header.Text(key)
	if err != nil {
		return header.Get(key)
	}
	return value
}

func messageAddresses(header *messagemail.Header, key string) []mailmodel.Address {
	values, err := header.AddressList(key)
	if err != nil {
		return nil
	}
	return standardAddresses(values, nil)
}

func standardAddresses(values []*mail.Address, err error) []mailmodel.Address {
	if err != nil {
		return nil
	}
	result := make([]mailmodel.Address, 0, len(values))
	for _, value := range values {
		result = append(result, mailmodel.Address{Name: mailmodel.DecodeHeaderText(value.Name), Email: value.Address})
	}
	return result
}

func readLimited(reader io.Reader, max int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > max {
		return nil, fmt.Errorf("MIME part exceeds %d bytes", max)
	}
	return value, nil
}
