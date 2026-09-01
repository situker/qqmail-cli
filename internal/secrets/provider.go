package secrets

import (
	"errors"
	"os"
	"strings"

	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/output"
	keyring "github.com/zalando/go-keyring"
)

const (
	ServiceName = "qqmailctl"
	EnvName     = "QQMAILCTL_AUTH_CODE"
)

type Provider interface {
	Get(account string) (string, error)
	Set(account, code string) error
	Delete(account string) error
	Source() string
}

type Keyring struct{}

func (Keyring) Get(account string) (string, error) {
	value, err := keyring.Get(ServiceName, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", &errmap.Error{Kind: errmap.AuthCodeMissing, Message: "系统凭据管理器中没有此账号的授权码", Suggestion: "重新运行 qqmailctl auth login"}
	}
	if err != nil {
		return "", &errmap.Error{Kind: errmap.Config, Message: "无法访问系统凭据管理器", Suggestion: "无头环境可显式使用 --auth-code-env", Cause: err}
	}
	output.RegisterSecret(value)
	return value, nil
}

func (Keyring) Set(account, code string) error {
	output.RegisterSecret(code)
	if err := keyring.Set(ServiceName, account, code); err != nil {
		return &errmap.Error{Kind: errmap.Config, Message: "无法写入系统凭据管理器", Cause: err}
	}
	return nil
}

func (Keyring) Delete(account string) error {
	err := keyring.Delete(ServiceName, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return &errmap.Error{Kind: errmap.Config, Message: "无法删除系统凭据", Cause: err}
	}
	return nil
}

func (Keyring) Source() string { return "keyring" }

type Environment struct{}

func (Environment) Get(_ string) (string, error) {
	value := strings.TrimSpace(os.Getenv(EnvName))
	if value == "" {
		return "", &errmap.Error{Kind: errmap.AuthCodeMissing, Message: EnvName + " 未设置"}
	}
	output.RegisterSecret(value)
	return value, nil
}

func (Environment) Set(_, _ string) error {
	return &errmap.Error{Kind: errmap.PolicyDenied, Message: "环境变量 provider 不允许持久化凭证"}
}

func (Environment) Delete(_ string) error { return nil }
func (Environment) Source() string        { return "env" }

type Memory struct{ Values map[string]string }

func (m *Memory) Get(account string) (string, error) {
	value, ok := m.Values[account]
	if !ok {
		return "", &errmap.Error{Kind: errmap.AuthCodeMissing, Message: "missing test secret"}
	}
	output.RegisterSecret(value)
	return value, nil
}
func (m *Memory) Set(account, code string) error {
	if m.Values == nil {
		m.Values = make(map[string]string)
	}
	m.Values[account] = code
	output.RegisterSecret(code)
	return nil
}
func (m *Memory) Delete(account string) error { delete(m.Values, account); return nil }
func (m *Memory) Source() string              { return "memory" }
