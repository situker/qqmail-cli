package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/mailmodel"
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
func (fakeReader) FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error) { return nil, nil }
func (fakeReader) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (fakeReader) Logout(context.Context) error { return nil }

func TestCommandTreeMatchesReadonlyWhitelist(t *testing.T) {
	rt := &Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader("")}
	root := NewRoot(rt)
	got := leafCommandNames(root)
	want := readonlyCommandNames()
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command surface changed; review readonly guarantee\ngot:  %v\nwant: %v", got, want)
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
		"personal": {Email: "user@qq.com"},
	}}
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(t.TempDir(), "cache.db")
	for _, tc := range []struct {
		args   []string
		schema string
	}{
		{[]string{"--config", configPath, "--json", "sync"}, "sync.schema.json"},
		{[]string{"--config", configPath, "--json", "search", "hello", "--local"}, "search.schema.json"},
	} {
		var out bytes.Buffer
		rt := &Runtime{
			Build: BuildInfo{Version: "test"}, Out: &out, Err: &bytes.Buffer{}, In: strings.NewReader(""),
			Secrets: &secrets.Memory{Values: map[string]string{"user@qq.com": "abcdefghijklmnop"}},
			Dial:    func(context.Context, account.Named, string) (imapx.Reader, error) { return fakeReader{}, nil },
			IndexOpen: func(_ string, write bool) (*index.DB, error) {
				return index.OpenPath(cachePath, write)
			},
		}
		root := NewRoot(rt)
		root.SetArgs(tc.args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		validateOutput(t, tc.schema, out.Bytes())
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
