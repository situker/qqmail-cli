package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

// fakeCleanMutator satisfies all three verification gates for the message the
// backup fixture created (INBOX:1:1), advertises MOVE, and records moves.
type fakeCleanMutator struct {
	fakeRestoreMutator
	fixtureSize int64
	moved       *[]string
}

func (f fakeCleanMutator) Capabilities() ([]string, []string) {
	return []string{"IMAP4rev1"}, []string{"IMAP4rev1", "MOVE"}
}
func (f fakeCleanMutator) Examine(context.Context, string) (uint32, uint32, error) { return 1, 1, nil }
func (f fakeCleanMutator) FetchEnvelopes(_ context.Context, folder string, uidValidity uint32, ids []uint32) ([]mailmodel.Envelope, error) {
	return []mailmodel.Envelope{{UID: 1, Size: f.fixtureSize, Folder: folder, UIDValidity: uidValidity}}, nil
}
func (f fakeCleanMutator) FetchHeaderFields(context.Context, []uint32) ([]mailmodel.HeaderFields, error) {
	return []mailmodel.HeaderFields{{UID: 1, MessageID: "<fixture@example.com>"}}, nil
}
func (f fakeCleanMutator) MoveUID(_ context.Context, id mailmodel.MsgID, destination string) (imapx.MutationResult, error) {
	*f.moved = append(*f.moved, id.String()+"->"+destination)
	return imapx.MutationResult{Method: "uid_move", Destination: destination, DestinationVerified: true}, nil
}

func TestCleanExecuteEndToEnd(t *testing.T) {
	t.Setenv("QQMAILCTL_READONLY", "0")
	configPath, planPath, provider := restoreFixture(t)
	raw, _, err := fakeReader{}.FetchBodyPeek(context.Background(), mailmodel.MsgID{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	moved := []string{}
	var out, stderr bytes.Buffer
	rt := &Runtime{
		Out: &out, Err: &stderr, In: strings.NewReader("1\n"), Secrets: provider,
		DialMutator: func(context.Context, account.Named, string) (imapx.Mutator, error) {
			return fakeCleanMutator{fixtureSize: int64(len(raw)), moved: &moved}, nil
		},
		IndexOpen:  func(string, bool) (*index.DB, error) { return index.OpenPath(filepath.Join(t.TempDir(), "cache.db"), true) },
		IsTerminal: func(io.Reader) bool { return true },
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"--config", configPath, "--json", "clean", "--plan", planPath, "--execute"})
	if err := root.Execute(); err != nil {
		t.Fatalf("clean execute failed: %v (stderr=%s)", err, stderr.String())
	}
	validateOutput(t, "clean.schema.json", out.Bytes())
	var envelope struct {
		Data struct {
			Completed   int    `json:"completed"`
			Destination string `json:"destination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Completed != 1 || envelope.Data.Destination != "Deleted Messages" {
		t.Fatalf("unexpected clean output: %s", out.String())
	}
	if len(moved) != 1 || !strings.Contains(moved[0], "->Deleted Messages") {
		t.Fatalf("unexpected move calls: %v", moved)
	}
	// The confirmation prompt must have asked for the exact eligible count.
	if !strings.Contains(stderr.String(), "1") {
		t.Fatalf("confirmation prompt missing count: %s", stderr.String())
	}
}

func TestCleanExecuteRejectsOversizedBatch(t *testing.T) {
	t.Setenv("QQMAILCTL_READONLY", "0")
	configPath, planPath, provider := restoreFixture(t)
	var out, stderr bytes.Buffer
	rt := &Runtime{
		Out: &out, Err: &stderr, In: strings.NewReader("1\n"), Secrets: provider,
		DialMutator: func(context.Context, account.Named, string) (imapx.Mutator, error) {
			t.Fatal("batch-limit rejection must happen before dialing")
			return nil, nil
		},
		IsTerminal: func(io.Reader) bool { return true },
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"--config", configPath, "clean", "--plan", planPath, "--execute", "--batch-limit", "0"})
	if err := root.Execute(); err == nil {
		t.Fatal("invalid batch limit accepted")
	}
}
