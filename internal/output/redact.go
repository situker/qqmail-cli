package output

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

var (
	secretMu sync.RWMutex
	secrets  []string
	ansiRE   = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)
)

func RegisterSecret(secret string) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return
	}
	secretMu.Lock()
	defer secretMu.Unlock()
	secrets = append(secrets, secret, base64.StdEncoding.EncodeToString([]byte(secret)))
}

// SanitizeMarkdown neutralizes terminal controls, bidi overrides, line breaks,
// and Markdown punctuation in untrusted email fields before human review.
func SanitizeMarkdown(value string) string {
	value = SanitizeHuman(value)
	var builder strings.Builder
	for _, r := range value {
		if strings.ContainsRune(`\\`+"`*_{}[]()#+-.!|><&", r) || r == '\r' || r == '\n' {
			if r == '\r' || r == '\n' {
				builder.WriteByte(' ')
			} else {
				_, _ = fmt.Fprintf(&builder, "&#%d;", r)
			}
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func RedactString(value string) string {
	secretMu.RLock()
	defer secretMu.RUnlock()
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, "***")
	}
	return value
}

// SanitizeHuman removes terminal controls and bidi overrides from untrusted
// strings. JSON output deliberately preserves the original data.
func SanitizeHuman(value string) string {
	value = ansiRE.ReplaceAllString(value, "")
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' {
			return -1
		}
		if (r >= 0x7f && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return -1
		}
		return r
	}, value)
}
