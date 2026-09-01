package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/secrets"
)

// A re-login after an authorization-code reset must not wipe the account's
// existing configuration — the send allowlist is a safety gate.
func TestAuthLoginPreservesExistingAccountFields(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := &account.Config{}
	if err := cfg.Put("personal", account.Account{Email: "user@qq.com", SendAllowlist: []string{"boss@example.com"}, IMAPPort: 993}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	rt := &Runtime{
		Build: BuildInfo{Version: "test"}, Out: &out, Err: &stderr, In: strings.NewReader("abcdefghijklmnop\n"),
		Secrets: &secrets.Memory{},
		Dial:    func(context.Context, account.Named, string) (imapx.Reader, error) { return fakeReader{}, nil },
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"--config", configPath, "--json", "auth", "login", "--email", "user@qq.com", "--name", "personal", "--auth-code-stdin"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	reloaded, _, loadErr := account.Load(configPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	saved, ok := reloaded.Accounts["personal"]
	if !ok || len(saved.SendAllowlist) != 1 || saved.SendAllowlist[0] != "boss@example.com" {
		t.Fatalf("send allowlist lost on re-login: %+v", saved)
	}
}
