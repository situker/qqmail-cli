package account

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/situker/qqmail-cli/internal/errmap"
)

const ConfigSchema = 1

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type Account struct {
	Email         string   `toml:"email" json:"email"`
	IMAPHost      string   `toml:"imap_host,omitempty" json:"imap_host"`
	IMAPPort      int      `toml:"imap_port,omitempty" json:"imap_port"`
	SMTPHost      string   `toml:"smtp_host,omitempty" json:"smtp_host"`
	SMTPPort      int      `toml:"smtp_port,omitempty" json:"smtp_port"`
	SendAllowlist []string `toml:"send_allowlist,omitempty" json:"send_allowlist"`
}

func (a Account) Host() string {
	if a.IMAPHost == "" {
		return "imap.qq.com"
	}
	return a.IMAPHost
}

func (a Account) Port() int {
	if a.IMAPPort == 0 {
		return 993
	}
	return a.IMAPPort
}

func (a Account) Address() string { return fmt.Sprintf("%s:%d", a.Host(), a.Port()) }

func (a Account) SMTPAddress() string {
	host := a.SMTPHost
	if host == "" {
		host = "smtp.qq.com"
	}
	port := a.SMTPPort
	if port == 0 {
		port = 465
	}
	return fmt.Sprintf("%s:%d", host, port)
}

type Config struct {
	Schema         int                `toml:"schema" json:"schema"`
	DefaultAccount string             `toml:"default_account" json:"default_account"`
	Accounts       map[string]Account `toml:"accounts" json:"accounts"`
}

type Named struct {
	Name          string   `json:"name"`
	Email         string   `json:"email"`
	IMAPHost      string   `json:"imap_host"`
	IMAPPort      int      `json:"imap_port"`
	SMTPHost      string   `json:"smtp_host"`
	SMTPPort      int      `json:"smtp_port"`
	SendAllowlist []string `json:"send_allowlist"`
	IsDefault     bool     `json:"is_default"`
}

func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "qqmail-cli", "config.toml"), nil
}

func Load(path string) (*Config, string, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, "", &errmap.Error{Kind: errmap.Config, Message: "无法确定配置目录", Cause: err}
		}
	}
	cfg := &Config{Schema: ConfigSchema, Accounts: make(map[string]Account)}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, path, nil
	}
	if err != nil {
		return nil, path, &errmap.Error{Kind: errmap.Config, Message: "无法读取配置文件", Cause: err}
	}
	if err := toml.Unmarshal(raw, cfg); err != nil {
		return nil, path, &errmap.Error{Kind: errmap.Config, Message: "配置文件格式损坏", Cause: err}
	}
	if cfg.Schema != ConfigSchema {
		return nil, path, &errmap.Error{Kind: errmap.Config, Message: fmt.Sprintf("不支持的配置版本 %d", cfg.Schema)}
	}
	if cfg.Accounts == nil {
		cfg.Accounts = make(map[string]Account)
	}
	return cfg, path, nil
}

func (c *Config) Save(path string) error {
	if path == "" {
		return &errmap.Error{Kind: errmap.Config, Message: "配置路径为空"}
	}
	c.Schema = ConfigSchema
	raw, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (c *Config) Put(name string, value Account) error {
	if !validName.MatchString(name) {
		return &errmap.Error{Kind: errmap.Usage, Message: "账号名称仅允许字母、数字、点、下划线和连字符，最长 64 字符"}
	}
	value.Email = strings.TrimSpace(value.Email)
	if value.Email == "" || !strings.Contains(value.Email, "@") {
		return &errmap.Error{Kind: errmap.Usage, Message: "邮箱地址格式无效"}
	}
	if value.IMAPPort < 0 || value.IMAPPort > 65535 {
		return &errmap.Error{Kind: errmap.Usage, Message: "IMAP 端口无效"}
	}
	if value.SMTPPort < 0 || value.SMTPPort > 65535 {
		return &errmap.Error{Kind: errmap.Usage, Message: "SMTP 端口无效"}
	}
	if c.Accounts == nil {
		c.Accounts = make(map[string]Account)
	}
	c.Accounts[name] = value
	if c.DefaultAccount == "" {
		c.DefaultAccount = name
	}
	return nil
}

func (c *Config) Remove(name string) { delete(c.Accounts, name) }

func (c *Config) Use(name string) error {
	if _, ok := c.Accounts[name]; !ok {
		return &errmap.Error{Kind: errmap.Config, Message: "账号不存在：" + name}
	}
	c.DefaultAccount = name
	return nil
}

func (c *Config) Resolve(name string) (Named, error) {
	if name == "" {
		name = c.DefaultAccount
	}
	value, ok := c.Accounts[name]
	if !ok || name == "" {
		return Named{}, &errmap.Error{Kind: errmap.Config, Message: "尚未配置账号", Suggestion: "先运行 qqmail-cli auth login"}
	}
	smtpHost := value.SMTPHost
	if smtpHost == "" {
		smtpHost = "smtp.qq.com"
	}
	smtpPort := value.SMTPPort
	if smtpPort == 0 {
		smtpPort = 465
	}
	return Named{Name: name, Email: value.Email, IMAPHost: value.Host(), IMAPPort: value.Port(), SMTPHost: smtpHost, SMTPPort: smtpPort, SendAllowlist: append([]string(nil), value.SendAllowlist...), IsDefault: name == c.DefaultAccount}, nil
}

func (c *Config) List() []Named {
	names := make([]string, 0, len(c.Accounts))
	for name := range c.Accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Named, 0, len(names))
	for _, name := range names {
		named, _ := c.Resolve(name)
		result = append(result, named)
	}
	return result
}
