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
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/situker/qqmailctl/internal/secrets"
	"github.com/situker/qqmailctl/internal/sendmail"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type Runtime struct {
	Build        BuildInfo
	JSON         bool
	Account      string
	Folder       string
	Timeout      time.Duration
	Verbose      bool
	ConfigPath   string
	AuthCodeEnv  bool
	Out          io.Writer
	Err          io.Writer
	In           io.Reader
	Secrets      secrets.Provider
	Dial         func(context.Context, account.Named, string) (imapx.Reader, error)
	DialMutator  func(context.Context, account.Named, string) (imapx.Mutator, error)
	IndexOpen    func(string, bool) (*index.DB, error)
	IndexInspect func(string) (index.Inspection, error)
	IndexClear   func(string) ([]string, error)
	AuditAppend  func(string, index.AuditEntry) error
	AuditRead    func(string, int) ([]index.AuditEntry, error)
	SendMail     policy.MailTransport
	IsTerminal   func(io.Reader) bool
	started      time.Time
	current      string
	resultCode   int
}

func Execute(build BuildInfo) int {
	rt := &Runtime{Build: build, Folder: "INBOX", Timeout: 120 * time.Second, Out: os.Stdout, Err: os.Stderr, In: os.Stdin}
	rt.started = time.Now()
	root := NewRoot(rt)
	if err := root.Execute(); err != nil {
		failure, code := errmap.Details(err)
		// An unknown root command fails before flag parsing, so rt.JSON never
		// gets set; honor a --json on the raw argument list to keep stdout a
		// valid JSON document on every error path.
		if rt.JSON || argsRequestJSON(os.Args[1:]) {
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

// argsRequestJSON detects --json on the raw argument list. A literal "--json"
// value passed to another flag could false-positive here; that only affects
// the formatting of an already-failing invocation.
func argsRequestJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	return false
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
			return imapx.DialWithVersion(ctx, cfg, authCode, rt.Build.Version)
		}
	}
	if rt.IndexOpen == nil {
		rt.IndexOpen = index.Open
	}
	if rt.IndexInspect == nil {
		rt.IndexInspect = index.InspectAccount
	}
	if rt.IndexClear == nil {
		rt.IndexClear = index.Clear
	}
	if rt.AuditAppend == nil {
		rt.AuditAppend = index.AppendStandaloneAudit
	}
	if rt.AuditRead == nil {
		rt.AuditRead = index.ReadAuditJSONL
	}
	if rt.SendMail == nil {
		rt.SendMail = sendmail.Send
	}
	if rt.DialMutator == nil {
		rt.DialMutator = func(ctx context.Context, cfg account.Named, authCode string) (imapx.Mutator, error) {
			return imapx.DialMutatorWithVersion(ctx, cfg, authCode, rt.Build.Version)
		}
	}
	if rt.IsTerminal == nil {
		rt.IsTerminal = func(reader io.Reader) bool {
			file, ok := reader.(*os.File)
			return ok && term.IsTerminal(int(file.Fd()))
		}
	}
	root := &cobra.Command{
		Use:           "qqmailctl",
		Short:         "Unofficial safety-first QQ Mail CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			rt.started = time.Now()
			rt.current = commandName(cmd)
		},
	}
	root.SetOut(rt.Out)
	root.SetErr(rt.Err)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &errmap.Error{Kind: errmap.Usage, Message: "参数用法错误：" + err.Error(), Suggestion: "运行 qqmailctl --help 查看命令与参数"}
	})
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
		newSyncCommand(rt), newLocalSearchCommand(rt),
		newTriageCommand(rt), newBackupCommand(rt),
		newCleanCommand(rt), newRestoreCommand(rt),
		newCacheCommand(rt), newAuditCommand(rt), newWatchCommand(rt),
		newSendCommand(rt), newReplyCommand(rt), newForwardCommand(rt),
	)
	return root
}

// requireSubcommand turns a bare parent invocation or an unknown subcommand
// into a structured usage error. Without it cobra prints the parent's help to
// stdout and exits 0 — which in --json mode is both a stdout-purity violation
// and a false success.
func requireSubcommand(cmd *cobra.Command) *cobra.Command {
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) > 0 {
			return &errmap.Error{Kind: errmap.Usage, Message: fmt.Sprintf("未知子命令 %q", args[0]), Suggestion: fmt.Sprintf("运行 qqmailctl %s --help 查看可用子命令", c.Name())}
		}
		return &errmap.Error{Kind: errmap.Usage, Message: "缺少子命令", Suggestion: fmt.Sprintf("运行 qqmailctl %s --help 查看可用子命令", c.Name())}
	}
	return cmd
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

func (rt *Runtime) connectMutator(ctx context.Context) (imapx.Mutator, account.Named, error) {
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
	writer, err := rt.DialMutator(ctx, named, authCode)
	return writer, named, err
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
	rt.notePartial(warnings)
	return output.WriteJSON(rt.Out, output.Envelope{SchemaVersion: output.SchemaVersion, Command: commandName(cmd), OK: true, Data: data, Error: nil, Warnings: warnings, Meta: meta})
}

// notePartial makes exit code 70 independent of the output mode: a partially
// failed batch must signal partial success to scripts in text mode too.
func (rt *Runtime) notePartial(warnings []output.Warning) {
	if len(warnings) > 0 {
		rt.resultCode = output.ExitPartial
	}
}
