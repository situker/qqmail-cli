package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/secrets"
	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type Runtime struct {
	Build       BuildInfo
	JSON        bool
	Account     string
	Folder      string
	Timeout     time.Duration
	Verbose     bool
	ConfigPath  string
	AuthCodeEnv bool
	Out         io.Writer
	Err         io.Writer
	In          io.Reader
	Secrets     secrets.Provider
	Dial        func(context.Context, account.Named, string) (imapx.Reader, error)
	started     time.Time
	current     string
	resultCode  int
}

func Execute(build BuildInfo) int {
	rt := &Runtime{Build: build, Folder: "INBOX", Timeout: 120 * time.Second, Out: os.Stdout, Err: os.Stderr, In: os.Stdin}
	root := NewRoot(rt)
	if err := root.Execute(); err != nil {
		failure, code := errmap.Details(err)
		if rt.JSON {
			command := rt.current
			if command == "" {
				command = "root"
			}
			_ = output.WriteJSON(rt.Out, output.Failure(command, failure, rt.started))
		} else {
			_, _ = fmt.Fprintln(rt.Err, output.RedactString(failure.Message))
			if failure.Suggestion != "" {
				_, _ = fmt.Fprintln(rt.Err, output.RedactString("建议："+failure.Suggestion))
			}
		}
		return code
	}
	return rt.resultCode
}

func NewRoot(rt *Runtime) *cobra.Command {
	if rt.In == nil {
		rt.In = os.Stdin
	}
	if rt.Secrets == nil {
		rt.Secrets = secrets.Keyring{}
	}
	if rt.Dial == nil {
		rt.Dial = func(ctx context.Context, cfg account.Named, authCode string) (imapx.Reader, error) {
			return imapx.Dial(ctx, cfg, authCode)
		}
	}
	root := &cobra.Command{
		Use:           "qqmailctl",
		Short:         "Unofficial read-only QQ Mail CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			rt.started = time.Now()
			rt.current = commandName(cmd)
		},
	}
	root.SetOut(rt.Out)
	root.SetErr(rt.Err)
	root.PersistentFlags().BoolVar(&rt.JSON, "json", false, "write one machine-readable JSON document")
	root.PersistentFlags().StringVar(&rt.Account, "account", "", "account name (defaults to configured default)")
	root.PersistentFlags().StringVar(&rt.Folder, "folder", "INBOX", "mail folder")
	root.PersistentFlags().DurationVar(&rt.Timeout, "timeout", 120*time.Second, "overall timeout")
	root.PersistentFlags().BoolVar(&rt.Verbose, "verbose", false, "write diagnostic details to stderr")
	root.PersistentFlags().StringVar(&rt.ConfigPath, "config", "", "override config file path")
	root.PersistentFlags().BoolVar(&rt.AuthCodeEnv, "auth-code-env", false, "explicitly allow QQMAILCTL_AUTH_CODE for this invocation")

	root.AddCommand(
		newVersionCommand(rt), newCompletionCommand(root), newAuthCommand(rt), newAccountCommand(rt),
		newFolderCommand(rt), newEnvelopeCommand(rt), newMessageCommand(rt), newAttachmentCommand(rt),
		newExportCommand(rt), newDoctorCommand(rt), newAgentInfoCommand(rt), newSchemaCommand(rt),
	)
	return root
}

func commandName(cmd *cobra.Command) string {
	if cmd == nil || cmd.CommandPath() == "qqmailctl" {
		return "root"
	}
	path := cmd.CommandPath()
	if len(path) > len("qqmailctl ") {
		path = path[len("qqmailctl "):]
	}
	for i := range path {
		if path[i] == ' ' {
			path = path[:i] + "." + path[i+1:]
		}
	}
	return path
}

func (rt *Runtime) context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), rt.Timeout)
}

func (rt *Runtime) loadAccount() (*account.Config, string, account.Named, error) {
	cfg, path, err := account.Load(rt.ConfigPath)
	if err != nil {
		return nil, path, account.Named{}, err
	}
	named, err := cfg.Resolve(rt.Account)
	return cfg, path, named, err
}

func (rt *Runtime) connect(ctx context.Context) (imapx.Reader, account.Named, error) {
	_, _, named, err := rt.loadAccount()
	if err != nil {
		return nil, named, err
	}
	provider := rt.Secrets
	if rt.AuthCodeEnv {
		provider = secrets.Environment{}
		_, _ = fmt.Fprintln(rt.Err, "凭证来源：环境变量（仅限本次调用）")
	}
	authCode, err := provider.Get(named.Email)
	if err != nil {
		return nil, named, err
	}
	reader, err := rt.Dial(ctx, named, authCode)
	return reader, named, err
}

func writeResult(rt *Runtime, cmd *cobra.Command, data any, text func(io.Writer) error) error {
	if rt.JSON {
		return output.WriteJSON(rt.Out, output.Success(commandName(cmd), data, rt.started))
	}
	if text != nil {
		return text(rt.Out)
	}
	return nil
}

func writeDetailed(rt *Runtime, cmd *cobra.Command, data any, warnings []output.Warning, meta output.Meta) error {
	if meta.DurationMS == 0 {
		meta.DurationMS = time.Since(rt.started).Milliseconds()
	}
	if warnings == nil {
		warnings = []output.Warning{}
	}
	if len(warnings) > 0 {
		rt.resultCode = output.ExitPartial
	}
	return output.WriteJSON(rt.Out, output.Envelope{SchemaVersion: output.SchemaVersion, Command: commandName(cmd), OK: true, Data: data, Error: nil, Warnings: warnings, Meta: meta})
}
