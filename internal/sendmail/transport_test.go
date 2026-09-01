package sendmail

import (
	"errors"
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

func TestSMTPTLSMapping(t *testing.T) {
	err := classifySMTP(errors.New("tls: failed to verify certificate"))
	if classified := errmap.Classify(err); classified.Kind != errmap.TLS {
		t.Fatalf("unexpected mapping: %+v", classified)
	}
}
