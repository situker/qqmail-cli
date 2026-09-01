package cli

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/secrets"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

func newDoctorCommand(rt *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "Diagnose configuration, credentials, TLS, login, and IMAP capabilities"}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		checks := []doctorCheck{}
		start := time.Now()
		_, _, named, err := rt.loadAccount()
		if err != nil {
			return err
		}
		checks = append(checks, doctorCheck{Name: "config", Status: "pass", DurationMS: time.Since(start).Milliseconds(), Detail: named.Name + " <" + named.Email + ">"})
		start = time.Now()
		provider := rt.Secrets
		if rt.AuthCodeEnv {
			provider = secrets.Environment{}
		}
		authCode, err := provider.Get(named.Email)
		if err != nil {
			return err
		}
		checks = append(checks, doctorCheck{Name: "credential", Status: "pass", DurationMS: time.Since(start).Milliseconds(), Detail: provider.Source()})
		start = time.Now()
		ctx, cancel := rt.context()
		defer cancel()
		reader, err := rt.Dial(ctx, named, authCode)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Logout(context.Background()) }()
		before, after := reader.Capabilities()
		checks = append(checks, doctorCheck{Name: "tls_login", Status: "pass", DurationMS: time.Since(start).Milliseconds(), Detail: named.IMAPHost + ":" + fmt.Sprint(named.IMAPPort)})
		checks = append(checks, doctorCheck{Name: "capability", Status: "pass", DurationMS: 0, Detail: fmt.Sprintf("before=%v after=%v", before, after)})
		warnings := []output.Warning{}
		if named.IMAPHost != "imap.qq.com" || named.IMAPPort != 993 {
			warnings = append(warnings, output.Warning{Code: "custom_server", Message: "当前账号使用非默认 IMAP 服务器，请确认配置未被篡改", Retryable: false})
		}
		if runtime.GOOS == "windows" {
			checks = append(checks, doctorCheck{Name: "console_utf8", Status: "info", Detail: "PowerShell 5.1 中文异常时设置 [Console]::OutputEncoding = [Text.Encoding]::UTF8"})
		}
		data := map[string]any{"checks": checks, "capabilities_before_login": before, "capabilities_after_login": after, "readonly": true, "collection_scope_note": "网页端收取选项会影响 IMAP 可见范围；具体影响待专用测试账号实测"}
		if rt.JSON {
			return writeDetailed(rt, cmd, data, warnings, output.Meta{Account: named.Name})
		}
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			for _, check := range checks {
				if _, err := fmt.Fprintf(w, "%-18s %-5s %s\n", check.Name, check.Status, check.Detail); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return cmd
}
