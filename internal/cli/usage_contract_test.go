package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/situker/qqmail-cli/internal/errmap"
	"github.com/situker/qqmail-cli/internal/output"
)

// Every cobra-level usage failure must classify as usage/exit 2 and must not
// leak help text onto stdout: in --json mode stdout is a contract surface.
func TestCobraUsageErrorsAreUsageContractFailures(t *testing.T) {
	cases := [][]string{
		{"--json", "auth", "bogus-subcommand"}, // unknown subcommand
		{"--json", "auth"},                     // bare parent
		{"--json", "login"},                    // unknown root command (contains the string "login")
		{"--json", "version", "--bogus-flag"},  // unknown flag
		{"--json", "version", "extra-arg"},     // extra positional argument
		{"--json", "clean"},                    // missing required flag
		{"--json", "triage"},                   // bare parent with scoped children
	}
	for _, args := range cases {
		var out, stderr bytes.Buffer
		rt := &Runtime{Out: &out, Err: &stderr, In: strings.NewReader("")}
		root := NewRoot(rt)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil {
			t.Fatalf("args %v unexpectedly succeeded (stdout=%q)", args, out.String())
		}
		failure, code := errmap.Details(err)
		if failure.Code != "usage" || code != output.ExitUsage {
			t.Fatalf("args %v: code=%s exit=%d, want usage/%d (err=%v)", args, failure.Code, code, output.ExitUsage, err)
		}
		if strings.Contains(out.String(), "Usage:") || strings.Contains(out.String(), "Available Commands") {
			t.Fatalf("args %v leaked help text onto stdout: %q", args, out.String())
		}
	}
}

func TestArgsRequestJSONDetection(t *testing.T) {
	if !argsRequestJSON([]string{"badcmd", "--json"}) {
		t.Fatal("--json after an unknown command was not detected")
	}
	if argsRequestJSON([]string{"envelope", "list", "--", "--json"}) {
		t.Fatal("--json after the terminator must not count")
	}
}

func TestUnknownServerErrorNoLongerGuessesAuth(t *testing.T) {
	// A cobra-style unknown-command error containing the word "login" must be
	// usage, never auth_failed with a misleading authorization-code hint.
	err := errmap.Classify(strings2error(`unknown command "login" for "qqmail-cli"`))
	if err.Kind != errmap.Usage {
		t.Fatalf("kind=%s, want usage", err.Kind)
	}
}

type strings2error string

func (s strings2error) Error() string { return string(s) }
