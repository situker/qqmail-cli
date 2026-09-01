package sendmail

import (
	"testing"

	smtp "github.com/emersion/go-smtp"
	"github.com/situker/qqmailctl/internal/errmap"
)

func TestSMTPRateLimitMapping(t *testing.T) {
	err := classifySMTP(&smtp.SMTPError{Code: 454, Message: "too many messages"})
	classified := errmap.Classify(err)
	if classified.Kind != errmap.RateLimited || classified.Suggestion == "" {
		t.Fatalf("unexpected mapping: %+v", classified)
	}
}
