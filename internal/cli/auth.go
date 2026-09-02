package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/errmap"
	"github.com/situker/qqmail-cli/internal/output"
	"github.com/situker/qqmail-cli/internal/policy"
	"github.com/situker/qqmail-cli/internal/secrets"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var authCodePattern = regexp.MustCompile(`^[A-Za-z0-9]{16}$`)

func newAuthCommand(rt *Runtime) *cobra.Command {
	auth := requireSubcommand(&cobra.Command{Use: "auth", Short: "Manage QQ Mail authorization"})
	auth.AddCommand(newAuthLoginCommand(rt), newAuthStatusCommand(rt), newAuthLogoutCommand(rt))
	return auth
}

func newAuthLoginCommand(rt *Runtime) *cobra.Command {
	var email, name string
	var fromStdin bool
	cmd := &cobra.Command{Use: "login", Short: "Validate and securely store an authorization code", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&email, "email", "", "QQ Mail address")
	cmd.Flags().StringVar(&name, "name", "personal", "local account name")
	cmd.Flags().BoolVar(&fromStdin, "auth-code-stdin", false, "read the authorization code from standard input")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		if email == "" {
			if rt.JSON {
				return &errmap.Error{Kind: errmap.Usage, Message: "--json 模式下必须提供 --email"}
			}
			_, _ = fmt.Fprint(rt.Err, "QQ 邮箱地址: ")
			value, err := bufio.NewReader(rt.In).ReadString('\n')
			if err != nil && err != io.EOF {
				return err
			}
			email = strings.TrimSpace(value)
		}
		domain := ""
		if at := strings.LastIndex(email, "@"); at >= 0 {
			domain = strings.ToLower(email[at+1:])
		}
		if domain != "qq.com" && domain != "foxmail.com" && domain != "vip.qq.com" {
			_, _ = fmt.Fprintln(rt.Err, "警告：这不是常见 QQ 邮箱域名；当前仍将连接 imap.qq.com")
		}
		if !fromStdin && !rt.JSON {
			_, _ = fmt.Fprintln(rt.Err, "请先在 QQ 邮箱网页端开启 IMAP，并生成 16 位授权码：")
			_, _ = fmt.Fprintln(rt.Err, "https://service.mail.qq.com/detail/0/1087")
		}
		authCode, err := readAuthCode(rt, fromStdin)
		if err != nil {
			return err
		}
		output.RegisterSecret(authCode)
		if !authCodePattern.MatchString(authCode) {
			return &errmap.Error{Kind: errmap.Usage, Message: "授权码应为 16 位字母或数字（可去掉分隔空格后重试）"}
		}
		cfg, path, err := account.Load(rt.ConfigPath)
		if err != nil {
			return err
		}
		// Re-login must not wipe an existing account's configuration: the send
		// allowlist and any host overrides survive an authorization-code reset.
		candidate := account.Account{Email: email}
		if existing, ok := cfg.Accounts[name]; ok {
			candidate = existing
			candidate.Email = email
		}
		if err := cfg.Put(name, candidate); err != nil {
			return err
		}
		named, err := cfg.Resolve(name)
		if err != nil {
			return err
		}
		ctx, cancel := rt.context()
		defer cancel()
		reader, err := rt.Dial(ctx, named, authCode)
		if err != nil {
			return err
		}
		if err := reader.Logout(ctx); err != nil && rt.Verbose {
			_, _ = fmt.Fprintln(rt.Err, output.RedactString("LOGOUT 警告："+err.Error()))
		}
		if err := rt.Secrets.Set(email, authCode); err != nil {
			return err
		}
		if err := cfg.Save(path); err != nil {
			_ = rt.Secrets.Delete(email)
			return err
		}
		data := map[string]any{"name": name, "email": email, "server": named.IMAPHost + ":" + fmt.Sprint(named.IMAPPort), "credential_source": rt.Secrets.Source(), "verified": true}
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "账号 %s 已验证并保存。\n", name)
			return err
		})
	}
	return cmd
}

func readAuthCode(rt *Runtime, fromStdin bool) (string, error) {
	if fromStdin {
		value, err := bufio.NewReader(rt.In).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		return strings.ReplaceAll(strings.TrimSpace(value), " ", ""), nil
	}
	file, ok := rt.In.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return "", &errmap.Error{Kind: errmap.Usage, Message: "标准输入不是交互式终端；请使用 --auth-code-stdin 安全注入"}
	}
	_, _ = fmt.Fprint(rt.Err, "授权码（输入不回显）: ")
	raw, err := term.ReadPassword(int(file.Fd()))
	_, _ = fmt.Fprintln(rt.Err)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(strings.TrimSpace(string(raw)), " ", ""), nil
}

func newAuthStatusCommand(rt *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "status", Short: "Show local authorization status", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		_, path, named, err := rt.loadAccount()
		if err != nil {
			return err
		}
		provider := rt.Secrets
		if rt.AuthCodeEnv {
			provider = secrets.Environment{}
		}
		_, secretErr := provider.Get(named.Email)
		present := secretErr == nil
		data := map[string]any{"name": named.Name, "email": named.Email, "configured": true, "credential_present": present, "credential_source": provider.Source(), "config_path": path, "custom_server": named.IMAPHost != "imap.qq.com" || named.IMAPPort != 993}
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "%s <%s> configured=%t credential=%t (%s)\n", named.Name, named.Email, true, present, provider.Source())
			return err
		})
	}
	return cmd
}

func newAuthLogoutCommand(rt *Runtime) *cobra.Command {
	var name string
	cmd := &cobra.Command{Use: "logout", Short: "Delete a locally stored credential and account reference", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&name, "name", "", "account name")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		cfg, path, err := account.Load(rt.ConfigPath)
		if err != nil {
			return err
		}
		named, err := cfg.Resolve(name)
		if err != nil {
			return err
		}
		if err := rt.Secrets.Delete(named.Email); err != nil {
			return err
		}
		cfg.Remove(named.Name)
		if cfg.DefaultAccount == named.Name {
			cfg.DefaultAccount = ""
			for _, remaining := range cfg.List() {
				cfg.DefaultAccount = remaining.Name
				break
			}
		}
		if err := cfg.Save(path); err != nil {
			return err
		}
		data := map[string]any{"name": named.Name, "local_credential_deleted": true, "remote_authorization_revoked": false}
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "本地凭据已删除。如需彻底作废，请到 QQ 邮箱网页端的授权码管理停用该授权码。")
			return err
		})
	}
	return cmd
}
