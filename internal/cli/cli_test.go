package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/cleanupplan"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/situker/qqmailctl/internal/secrets"
	projectschemas "github.com/situker/qqmailctl/schemas"
	"github.com/spf13/cobra"
)

type fakeReader struct{}

func (fakeReader) Capabilities() ([]string, []string) {
	return []string{"IMAP4rev1"}, []string{"IMAP4rev1"}
}
func (fakeReader) ListFolders(context.Context) ([]mailmodel.Folder, error)      { return nil, nil }
func (fakeReader) Examine(context.Context, string) (uint32, uint32, error)      { return 1, 0, nil }
func (fakeReader) Search(context.Context, imapx.SearchFilter) ([]uint32, error) { return nil, nil }
func (fakeReader) FetchEnvelopes(context.Context, string, uint32, []uint32) ([]mailmodel.Envelope, error) {
	return nil, nil
}
func (fakeReader) FetchHeaderFields(context.Context, []uint32) ([]mailmodel.HeaderFields, error) {
	return nil, nil
}
func (fakeReader) FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error) {
	return []byte("From: sender@example.com\r\n" +
		"Subject: fixture\r\n" +
		"Message-ID: <fixture@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n" +
		"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nbody\r\n" +
		"--b1\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"data.bin\"\r\nContent-Transfer-Encoding: base64\r\n\r\nAAEC\r\n" +
		"--b1--\r\n"), nil
}
func (fakeReader) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	return []byte("From: sender@example.com\r\nSubject: fixture\r\nMessage-ID: <fixture@example.com>\r\n\r\nbody\r\n"), false, nil
}
func (fakeReader) Logout(context.Context) error { return nil }

func TestCommandTreeMatchesDeclaredRiskCatalog(t *testing.T) {
	rt := &Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader("")}
	root := NewRoot(rt)
	got := leafCommandNames(root)
	want := commandNames()
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command surface changed; review risk catalog\ngot:  %v\nwant: %v", got, want)
	}
	for _, path := range []string{"clean", "restore", "message mark-read", "message move", "send", "reply", "forward"} {
		command, _, err := root.Find(strings.Fields(path))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"yes", "force", "no-confirm", "bypass"} {
			if command.Flags().Lookup(forbidden) != nil {
				t.Fatalf("unsafe confirmation bypass --%s appeared on %s", forbidden, path)
			}
		}
	}
}

func TestCacheClearHelpExplainsAuditRetention(t *testing.T) {
	rt := &Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader("")}
	command, _, err := NewRoot(rt).Find([]string{"cache", "clear"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command.Short, "retaining the audit JSONL") {
		t.Fatalf("cache clear help must explain retained audit data: %q", command.Short)
	}
}

func leafCommandNames(root *cobra.Command) []string {
	var result []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		children := cmd.Commands()
		if len(children) == 0 && cmd != root {
			result = append(result, commandName(cmd))
			return
		}
		for _, child := range children {
			if child.Name() != "help" {
				walk(child)
			}
		}
	}
	walk(root)
	sort.Strings(result)
	return result
}

func TestAuthLoginNeverWritesSecretToConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	provider := &secrets.Memory{}
	var out, stderr bytes.Buffer
	rt := &Runtime{
		Build: BuildInfo{Version: "test"}, Out: &out, Err: &stderr, In: strings.NewReader("abcdefghijklmnop\n"),
		Secrets: provider,
		Dial:    func(context.Context, account.Named, string) (imapx.Reader, error) { return fakeReader{}, nil },
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"--config", configPath, "--json", "auth", "login", "--email", "user@qq.com", "--name", "personal", "--auth-code-stdin"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "abcdefghijklmnop") || strings.Contains(strings.ToLower(string(raw)), "auth_code") {
		t.Fatalf("secret leaked into config: %s", raw)
	}
	if !strings.Contains(out.String(), `"verified":true`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local)
	got, err := parseSince("7d", now)
	if err != nil || !got.Equal(now.Add(-7*24*time.Hour)) {
		t.Fatalf("7d: %v, %v", got, err)
	}
	got, err = parseSince("2026-08-01", now)
	if err != nil || got.Day() != 1 || got.Month() != time.August {
		t.Fatalf("date: %v, %v", got, err)
	}
}

func TestVersionAndAgentInfoMatchSchemas(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		schema string
	}{
		{[]string{"--json", "version"}, "version.schema.json"},
		{[]string{"agent-info"}, "agent-info.schema.json"},
	} {
		var out bytes.Buffer
		rt := &Runtime{Build: BuildInfo{Version: "test", Commit: "fixture", Date: "2026-09-01"}, Out: &out, Err: &bytes.Buffer{}, In: strings.NewReader("")}
		root := NewRoot(rt)
		root.SetArgs(tc.args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		validateOutput(t, tc.schema, out.Bytes())
	}
}

func TestSyncAndLocalSearchMatchSchemas(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := &account.Config{Schema: account.ConfigSchema, DefaultAccount: "personal", Accounts: map[string]account.Account{
		"personal": {Email: "user@qq.com", SendAllowlist: []string{"sender@example.com", "reader@example.com"}},
	}}
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(t.TempDir(), "cache.db")
	planPath := filepath.Join(t.TempDir(), "plan.json")
	markdownPath := filepath.Join(t.TempDir(), "plan.md")
	backupPlanPath := filepath.Join(t.TempDir(), "backup-plan.json")
	backupDir := filepath.Join(t.TempDir(), "backup")
	backupID := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}.String()
	if err := cleanupplan.Save(backupPlanPath, cleanupplan.Plan{
		Schema: cleanupplan.Schema, CreatedAt: time.Now().UTC(),
		Items:      []cleanupplan.Item{{ID: backupID, Category: "other", Reason: "fixture", Evidence: []string{"fixture"}, From: []mailmodel.Address{}, Subject: "fixture", Date: time.Now().UTC(), SizeBytes: 1}},
		Statistics: cleanupplan.Statistics{TotalCount: 1, TotalSizeBytes: 1, ByCategory: map[string]int{"other": 1}, ByFromDomain: map[string]int{"example.com": 1}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args   []string
		schema string
	}{
		{[]string{"--config", configPath, "--json", "sync"}, "sync.schema.json"},
		{[]string{"--config", configPath, "--json", "search", "hello", "--local"}, "search.schema.json"},
		{[]string{"--config", configPath, "--json", "triage", "analyze"}, "triage.analyze.schema.json"},
		{[]string{"--config", configPath, "--json", "triage", "plan", "--output", planPath, "--markdown", markdownPath}, "triage.plan.schema.json"},
		{[]string{"--config", configPath, "--json", "backup", "--plan", backupPlanPath, "--output", backupDir}, "backup.schema.json"},
		{[]string{"--config", configPath, "--json", "message", "mark-read", backupID}, "message.mark-read.schema.json"},
		{[]string{"--config", configPath, "--json", "message", "move", backupID, "Trash"}, "message.move.schema.json"},
		{[]string{"--config", configPath, "--json", "clean", "--plan", backupPlanPath}, "clean.schema.json"},
		{[]string{"--config", configPath, "--json", "cache", "inspect"}, "cache.inspect.schema.json"},
		{[]string{"--config", configPath, "--json", "cache", "clear"}, "cache.clear.schema.json"},
		{[]string{"--config", configPath, "--json", "audit", "list"}, "audit.list.schema.json"},
		{[]string{"--config", configPath, "--json", "send", "--to", "reader@example.com", "--subject", "fixture", "--body", "hello"}, "send.schema.json"},
		{[]string{"--config", configPath, "--json", "reply", backupID, "--body", "thanks"}, "reply.schema.json"},
		{[]string{"--config", configPath, "--json", "forward", backupID, "--to", "reader@example.com", "--body", "FYI"}, "forward.schema.json"},
	} {
		var out bytes.Buffer
		rt := &Runtime{
			Build: BuildInfo{Version: "test"}, Out: &out, Err: &bytes.Buffer{}, In: strings.NewReader(""),
			Secrets: &secrets.Memory{Values: map[string]string{"user@qq.com": "abcdefghijklmnop"}},
			Dial:    func(context.Context, account.Named, string) (imapx.Reader, error) { return fakeReader{}, nil },
			IndexOpen: func(_ string, write bool) (*index.DB, error) {
				return index.OpenPath(cachePath, write)
			},
			IndexInspect: func(string) (index.Inspection, error) {
				store, err := index.OpenPath(cachePath, false)
				if err != nil {
					return index.Inspection{}, err
				}
				defer func() { _ = store.Close() }()
				return store.Inspect(context.Background())
			},
			AuditRead: func(string, int) ([]index.AuditEntry, error) { return []index.AuditEntry{}, nil },
		}
		root := NewRoot(rt)
		root.SetArgs(tc.args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		validateOutput(t, tc.schema, out.Bytes())
	}
}

func TestReadonlyEnvironmentBlocksEveryMutatingCommandBeforeDial(t *testing.T) {
	t.Setenv("QQMAILCTL_READONLY", "1")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cfg := &account.Config{Schema: account.ConfigSchema, DefaultAccount: "personal", Accounts: map[string]account.Account{"personal": {Email: "user@qq.com"}}}
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}.String()
	planPath := filepath.Join(dir, "plan.json")
	// The plan is deliberately non-empty: an empty plan would be rejected by
	// clean's own "empty plan" guard with the same PolicyDenied kind, which
	// once let this matrix pass with the readonly gate deleted.
	planItem := cleanupplan.Item{ID: id, Category: "marketing", Reason: "fixture", Evidence: []string{"header:List-Unsubscribe"}, From: []mailmodel.Address{{Email: "news@example.com"}}, Subject: "fixture", Date: time.Now(), SizeBytes: 1}
	if err := cleanupplan.Save(planPath, cleanupplan.Plan{Schema: 1, CreatedAt: time.Now(), Items: []cleanupplan.Item{planItem}, Statistics: cleanupplan.Statistics{TotalCount: 1, TotalSizeBytes: 1, ByCategory: map[string]int{"marketing": 1}, ByFromDomain: map[string]int{"example.com": 1}}}); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"--config", configPath, "auth", "login", "--email", "user@qq.com", "--auth-code-stdin"},
		{"--config", configPath, "auth", "logout", "--name", "personal"},
		{"--config", configPath, "account", "use", "personal"},
		{"--config", configPath, "attachment", "download", id, "all", "--output", dir},
		{"--config", configPath, "export", "--all", "--output", dir},
		{"--config", configPath, "sync"},
		{"--config", configPath, "triage", "plan", "--output", filepath.Join(dir, "new-plan.json")},
		{"--config", configPath, "backup", "--plan", planPath, "--output", dir},
		{"--config", configPath, "message", "mark-read", id, "--execute"},
		{"--config", configPath, "message", "move", id, "Trash", "--execute"},
		{"--config", configPath, "clean", "--plan", planPath, "--execute"},
		{"--config", configPath, "restore", "--plan", planPath, "--execute"},
		{"--config", configPath, "cache", "clear", "--execute"},
		{"--config", configPath, "send", "--to", "reader@example.com", "--subject", "fixture", "--execute"},
		{"--config", configPath, "reply", id, "--execute"},
		{"--config", configPath, "forward", id, "--to", "reader@example.com", "--execute"},
	}
	for _, args := range commands {
		dialed := false
		rt := &Runtime{
			Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader("abcdefghijklmnop\n1\nCLEAR\n"),
			Secrets: &secrets.Memory{Values: map[string]string{"user@qq.com": "abcdefghijklmnop"}},
			Dial: func(context.Context, account.Named, string) (imapx.Reader, error) {
				dialed = true
				return fakeReader{}, nil
			},
			IndexInspect: func(string) (index.Inspection, error) {
				return index.Inspection{Path: filepath.Join(dir, "cache.db")}, nil
			},
			IsTerminal: func(io.Reader) bool { return true },
		}
		root := NewRoot(rt)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil || errmap.Classify(err).Kind != errmap.PolicyDenied || dialed {
			t.Fatalf("command %v not blocked before dial: err=%v dialed=%v", args, err, dialed)
		}
		// The rejection must come from the readonly gate itself — not from a
		// sibling guard that happens to share the PolicyDenied kind.
		if !strings.Contains(err.Error(), "QQMAILCTL_READONLY") {
			t.Fatalf("command %v rejected by a non-readonly gate: %v", args, err)
		}
	}
}

func TestReadonlyUnknownValueFailsClosed(t *testing.T) {
	t.Setenv("QQMAILCTL_READONLY", "enabled")
	if !policy.Readonly() {
		t.Fatal("unrecognized truthy-looking value did not fail closed")
	}
	t.Setenv("QQMAILCTL_READONLY", "off")
	if policy.Readonly() {
		t.Fatal("explicit falsy value treated as readonly")
	}
}

func TestExecuteRejectsNonTTYBeforeMutationDial(t *testing.T) {
	t.Setenv("QQMAILCTL_READONLY", "0")
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}.String()
	dialed := false
	rt := &Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader("1\n"), IsTerminal: func(io.Reader) bool { return false }}
	rt.DialMutator = func(context.Context, account.Named, string) (imapx.Mutator, error) {
		dialed = true
		return nil, nil
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"message", "mark-read", id, "--execute"})
	err := root.Execute()
	if err == nil || errmap.Classify(err).Kind != errmap.PolicyDenied || dialed {
		t.Fatalf("non-TTY execution was not blocked before dial: err=%v dialed=%v", err, dialed)
	}
}

func validateOutput(t *testing.T, filename string, raw []byte) {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	base := "https://github.com/situker/qqmailctl/schemas/"
	for _, name := range projectschemas.Names() {
		document, err := projectschemas.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(document, &value); err != nil {
			t.Fatalf("schema %s: %v", name, err)
		}
		if err := compiler.AddResource(base+name, value); err != nil {
			t.Fatalf("add schema %s: %v", name, err)
		}
	}
	compiled, err := compiler.Compile(base + filename)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("invalid command JSON: %v\n%s", err, raw)
	}
	if err := compiled.Validate(value); err != nil {
		t.Fatalf("schema validation failed: %v\n%s", err, raw)
	}
}
