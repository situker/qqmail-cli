package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/situker/qqmailctl/internal/errmap"
)

// The count/token confirmations are the last human gate in front of every
// destructive action; all three states of each must hold.
func TestConfirmExactCountThreeStates(t *testing.T) {
	gate := func(input string, terminal bool) error {
		rt := &Runtime{Err: &bytes.Buffer{}, In: strings.NewReader(input), IsTerminal: func(io.Reader) bool { return terminal }}
		return confirmExactCount(rt, 3)
	}
	if err := gate("3\n", true); err != nil {
		t.Fatalf("correct count rejected: %v", err)
	}
	if err := gate("2\n", true); err == nil || errmap.Classify(err).Kind != errmap.PolicyDenied {
		t.Fatalf("wrong count accepted: %v", err)
	}
	if err := gate("", true); err == nil || errmap.Classify(err).Kind != errmap.PolicyDenied {
		t.Fatalf("EOF/empty input accepted: %v", err)
	}
	if err := gate("3\n", false); err == nil || errmap.Classify(err).Kind != errmap.PolicyDenied {
		t.Fatalf("non-TTY accepted: %v", err)
	}
}

func TestConfirmTokenThreeStates(t *testing.T) {
	gate := func(input string, terminal bool) error {
		rt := &Runtime{Err: &bytes.Buffer{}, In: strings.NewReader(input), IsTerminal: func(io.Reader) bool { return terminal }}
		return confirmToken(rt, "CLEAR")
	}
	if err := gate("CLEAR\n", true); err != nil {
		t.Fatalf("correct token rejected: %v", err)
	}
	if err := gate("clear\n", true); err == nil || errmap.Classify(err).Kind != errmap.PolicyDenied {
		t.Fatalf("wrong-case token accepted: %v", err)
	}
	if err := gate("", true); err == nil || errmap.Classify(err).Kind != errmap.PolicyDenied {
		t.Fatalf("EOF/empty input accepted: %v", err)
	}
	if err := gate("CLEAR\n", false); err == nil || errmap.Classify(err).Kind != errmap.PolicyDenied {
		t.Fatalf("non-TTY accepted: %v", err)
	}
}
