package secrets

import (
	"os"
	"testing"
)

func TestEnvironmentProvider(t *testing.T) {
	t.Setenv(EnvName, "abcdefghijklmnop")
	got, err := (Environment{}).Get("ignored")
	if err != nil || got != "abcdefghijklmnop" {
		t.Fatalf("got %q, %v", got, err)
	}
	_ = os.Unsetenv(EnvName)
}

func TestMemoryProviderLifecycle(t *testing.T) {
	p := &Memory{}
	if err := p.Set("user@qq.com", "abcdefghijklmnop"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get("user@qq.com"); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete("user@qq.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get("user@qq.com"); err == nil {
		t.Fatal("expected missing secret")
	}
}
