package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/cleanupplan"
	"github.com/situker/qqmail-cli/internal/imapx"
	"github.com/situker/qqmail-cli/internal/index"
	"github.com/situker/qqmail-cli/internal/mailmodel"
	"github.com/situker/qqmail-cli/internal/secrets"
)

type fakeRestoreMutator struct{ fakeReader }

func (fakeRestoreMutator) ListFolders(context.Context) ([]mailmodel.Folder, error) {
	return []mailmodel.Folder{{Name: "Deleted Messages", Attributes: []string{`\Trash`}}}, nil
}
func (fakeRestoreMutator) SetSeen(context.Context, mailmodel.MsgID) error { return nil }
func (fakeRestoreMutator) MoveUID(context.Context, mailmodel.MsgID, string) (imapx.MutationResult, error) {
	return imapx.MutationResult{Method: "uid_move", DestinationVerified: true}, nil
}
func (fakeRestoreMutator) CopyMarkDeletedUID(context.Context, mailmodel.MsgID, string, imapx.MessageIdentity) (imapx.MutationResult, error) {
	return imapx.MutationResult{Method: "copy_store_deleted", SourceRetained: true}, nil
}
func (fakeRestoreMutator) LocateByIdentity(_ context.Context, folder string, _ imapx.MessageIdentity) ([]mailmodel.MsgID, error) {
	return []mailmodel.MsgID{{Folder: folder, UIDValidity: 9, UID: 42}}, nil
}

func restoreFixture(t *testing.T) (string, string, *secrets.Memory) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cfg := &account.Config{Schema: account.ConfigSchema, DefaultAccount: "personal", Accounts: map[string]account.Account{"personal": {Email: "user@qq.com"}}}
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	provider := &secrets.Memory{Values: map[string]string{"user@qq.com": "abcdefghijklmnop"}}
	planPath := filepath.Join(dir, "plan.json")
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}.String()
	if err := cleanupplan.Save(planPath, cleanupplan.Plan{
		Schema: 1, CreatedAt: time.Now().UTC(),
		Items:      []cleanupplan.Item{{ID: id, Category: "marketing", Reason: "fixture", Evidence: []string{"fixture"}, From: []mailmodel.Address{}, Subject: "fixture", Date: time.Now().UTC(), SizeBytes: 1}},
		Statistics: cleanupplan.Statistics{TotalCount: 1, TotalSizeBytes: 1, ByCategory: map[string]int{"marketing": 1}, ByFromDomain: map[string]int{"(unknown)": 1}},
	}); err != nil {
		t.Fatal(err)
	}
	// Run the real backup flow so plan.json gains a backup_root with a
	// verified HMAC manifest — restore refuses anything less.
	var out, stderr bytes.Buffer
	rt := &Runtime{
		Out: &out, Err: &stderr, In: strings.NewReader(""), Secrets: provider,
		Dial: func(context.Context, account.Named, string) (imapx.Reader, error) { return fakeReader{}, nil },
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"--config", configPath, "--json", "backup", "--plan", planPath, "--output", filepath.Join(dir, "backup")})
	if err := root.Execute(); err != nil {
		t.Fatalf("backup fixture failed: %v (stderr=%s)", err, stderr.String())
	}
	return configPath, planPath, provider
}

func TestRestoreDryRunMatchesSchemaAndLocates(t *testing.T) {
	configPath, planPath, provider := restoreFixture(t)
	var out, stderr bytes.Buffer
	rt := &Runtime{
		Out: &out, Err: &stderr, In: strings.NewReader(""), Secrets: provider,
		DialMutator: func(context.Context, account.Named, string) (imapx.Mutator, error) { return fakeRestoreMutator{}, nil },
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"--config", configPath, "--json", "restore", "--plan", planPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("restore dry-run failed: %v (stderr=%s)", err, stderr.String())
	}
	validateOutput(t, "restore.schema.json", out.Bytes())
	var envelope struct {
		Data struct {
			DryRun  bool             `json:"dry_run"`
			Located []map[string]any `json:"located"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.DryRun || len(envelope.Data.Located) != 1 {
		t.Fatalf("unexpected restore dry-run: %s", out.String())
	}
}

func TestRestoreExecuteMovesBackAndMatchesSchema(t *testing.T) {
	t.Setenv("QQMAIL_CLI_READONLY", "0")
	configPath, planPath, provider := restoreFixture(t)
	var out, stderr bytes.Buffer
	rt := &Runtime{
		Out: &out, Err: &stderr, In: strings.NewReader("1\n"), Secrets: provider,
		DialMutator: func(context.Context, account.Named, string) (imapx.Mutator, error) { return fakeRestoreMutator{}, nil },
		IndexOpen:   func(string, bool) (*index.DB, error) { return index.OpenPath(filepath.Join(t.TempDir(), "cache.db"), true) },
		IsTerminal:  func(io.Reader) bool { return true },
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"--config", configPath, "--json", "restore", "--plan", planPath, "--execute"})
	if err := root.Execute(); err != nil {
		t.Fatalf("restore execute failed: %v (stderr=%s)", err, stderr.String())
	}
	validateOutput(t, "restore.schema.json", out.Bytes())
	var envelope struct {
		Data struct {
			Completed int `json:"completed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Completed != 1 {
		t.Fatalf("unexpected restore execute output: %s", out.String())
	}
}
