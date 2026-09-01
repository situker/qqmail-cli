package account

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRoundTripContainsNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &Config{Accounts: map[string]Account{}}
	if err := cfg.Put("personal", Account{Email: "user@qq.com"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("second save must replace config safely: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "auth_code") || strings.Contains(string(raw), "abcdefghijklmnop") {
		t.Fatalf("config contains secret field: %s", raw)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loaded.Resolve("")
	if err != nil || got.Email != "user@qq.com" || got.IMAPHost != "imap.qq.com" || got.IMAPPort != 993 {
		t.Fatalf("unexpected account: %#v, %v", got, err)
	}
}

func TestInvalidAccountName(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Put("../escape", Account{Email: "user@qq.com"}); err == nil {
		t.Fatal("expected invalid name error")
	}
}

func TestConfigSupportsUnicodePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "中文用户", "邮箱配置.toml")
	cfg := &Config{Accounts: map[string]Account{}}
	if err := cfg.Put("工作邮箱", Account{Email: "user@qq.com"}); err == nil {
		t.Fatal("non-ASCII account aliases are intentionally rejected for stable CLI use")
	}
	if err := cfg.Put("work", Account{Email: "user@qq.com"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err != nil {
		t.Fatal(err)
	}
}
