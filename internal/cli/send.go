package cli

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/errmap"
	"github.com/situker/qqmail-cli/internal/imapx"
	"github.com/situker/qqmail-cli/internal/mailmodel"
	"github.com/situker/qqmail-cli/internal/mimeparse"
	"github.com/situker/qqmail-cli/internal/output"
	"github.com/situker/qqmail-cli/internal/policy"
	"github.com/situker/qqmail-cli/internal/secrets"
	"github.com/situker/qqmail-cli/internal/sendmail"
	"github.com/spf13/cobra"
)

const maxComposeBodyBytes = 1 << 20

type composeOptions struct {
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Body        string
	BodyFile    string
	Attachments []string
	Execute     bool
}

type originalMessage struct {
	Parsed     mimeparse.Result
	ReplyTo    []mail.Address
	References []string
}

var messageIDPattern = regexp.MustCompile(`<([^<>\s]+)>`)

func newSendCommand(rt *Runtime) *cobra.Command {
	var opts composeOptions
	cmd := &cobra.Command{Use: "send", Short: "Compose mail; dry-run unless --execute is allowlisted and confirmed", Args: cobra.NoArgs}
	addComposeFlags(cmd, &opts, true, true)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if opts.Execute {
			if err := policy.RequireMutationAllowed(); err != nil {
				return err
			}
		}
		named, err := loadNamedAccount(rt)
		if err != nil {
			return err
		}
		draft, err := draftFromOptions(named, opts)
		if err != nil {
			return err
		}
		return runDraft(rt, cmd, named, draft, opts.Execute)
	}
	return cmd
}

func newReplyCommand(rt *Runtime) *cobra.Command {
	var opts composeOptions
	cmd := &cobra.Command{Use: "reply <id>", Args: cobra.ExactArgs(1), Short: "Reply with correct thread headers; dry-run by default"}
	addComposeFlags(cmd, &opts, false, false)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if opts.Execute {
			if err := policy.RequireMutationAllowed(); err != nil {
				return err
			}
		}
		id, err := mailmodel.ParseMsgID(args[0])
		if err != nil {
			return err
		}
		if err := requireFolderConsistency(cmd, rt, []mailmodel.MsgID{id}); err != nil {
			return err
		}
		named, err := loadNamedAccount(rt)
		if err != nil {
			return err
		}
		original, err := loadOriginal(rt, id)
		if err != nil {
			return err
		}
		body, err := composeBody(opts.Body, opts.BodyFile)
		if err != nil {
			return err
		}
		to := original.ReplyTo
		if len(to) == 0 {
			to = modelAddresses(original.Parsed.From)
		}
		if len(to) == 0 {
			return &errmap.Error{Kind: errmap.ParseError, Message: "原邮件没有可用的回复地址"}
		}
		cc, err := parseAddresses(opts.Cc)
		if err != nil {
			return err
		}
		bcc, err := parseAddresses(opts.Bcc)
		if err != nil {
			return err
		}
		attachments, err := loadAttachments(opts.Attachments)
		if err != nil {
			return err
		}
		subject := opts.Subject
		if subject == "" {
			subject = prefixedSubject(original.Parsed.Subject, "Re:")
		}
		quoted := quoteOriginal(original.Parsed)
		if body != "" {
			body += "\n\n"
		}
		body += quoted
		draft := sendmail.Draft{From: mail.Address{Address: named.Email}, To: to, Cc: cc, Bcc: bcc, Subject: subject, Body: body, Attachments: attachments, InReplyTo: original.Parsed.MessageID, References: original.References}
		return runDraft(rt, cmd, named, draft, opts.Execute)
	}
	return cmd
}

func newForwardCommand(rt *Runtime) *cobra.Command {
	var opts composeOptions
	cmd := &cobra.Command{Use: "forward <id>", Args: cobra.ExactArgs(1), Short: "Forward a quoted message and its attachments; dry-run by default"}
	addComposeFlags(cmd, &opts, true, false)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if opts.Execute {
			if err := policy.RequireMutationAllowed(); err != nil {
				return err
			}
		}
		id, err := mailmodel.ParseMsgID(args[0])
		if err != nil {
			return err
		}
		if err := requireFolderConsistency(cmd, rt, []mailmodel.MsgID{id}); err != nil {
			return err
		}
		named, err := loadNamedAccount(rt)
		if err != nil {
			return err
		}
		original, err := loadOriginal(rt, id)
		if err != nil {
			return err
		}
		body, err := composeBody(opts.Body, opts.BodyFile)
		if err != nil {
			return err
		}
		to, err := parseAddresses(opts.To)
		if err != nil {
			return err
		}
		cc, err := parseAddresses(opts.Cc)
		if err != nil {
			return err
		}
		bcc, err := parseAddresses(opts.Bcc)
		if err != nil {
			return err
		}
		attachments, err := loadAttachments(opts.Attachments)
		if err != nil {
			return err
		}
		for _, attachment := range original.Parsed.Attachments {
			attachments = append(attachments, sendmail.Attachment{Filename: attachment.Filename, ContentType: attachment.ContentType, Data: attachment.Data})
		}
		subject := opts.Subject
		if subject == "" {
			subject = prefixedSubject(original.Parsed.Subject, "Fwd:")
		}
		if body != "" {
			body += "\n\n"
		}
		body += forwardOriginal(original.Parsed)
		draft := sendmail.Draft{From: mail.Address{Address: named.Email}, To: to, Cc: cc, Bcc: bcc, Subject: subject, Body: body, Attachments: attachments, References: original.References}
		return runDraft(rt, cmd, named, draft, opts.Execute)
	}
	return cmd
}

func addComposeFlags(cmd *cobra.Command, opts *composeOptions, requireTo, requireSubject bool) {
	cmd.Flags().StringSliceVar(&opts.To, "to", nil, "recipient addresses")
	cmd.Flags().StringSliceVar(&opts.Cc, "cc", nil, "CC recipient addresses")
	cmd.Flags().StringSliceVar(&opts.Bcc, "bcc", nil, "BCC recipient addresses (never written to MIME headers)")
	cmd.Flags().StringVar(&opts.Subject, "subject", "", "message subject")
	cmd.Flags().StringVar(&opts.Body, "body", "", "plain-text body")
	cmd.Flags().StringVar(&opts.BodyFile, "body-file", "", "read plain-text body from a file (maximum 1 MiB)")
	cmd.Flags().StringSliceVar(&opts.Attachments, "attach", nil, "attachment paths (20 MiB combined maximum)")
	cmd.Flags().BoolVar(&opts.Execute, "execute", false, "send after allowlist validation and TTY confirmation")
	if requireTo {
		_ = cmd.MarkFlagRequired("to")
	}
	if requireSubject {
		_ = cmd.MarkFlagRequired("subject")
	}
}

func loadNamedAccount(rt *Runtime) (account.Named, error) {
	_, _, named, err := rt.loadAccount()
	return named, err
}

func draftFromOptions(named account.Named, opts composeOptions) (sendmail.Draft, error) {
	to, err := parseAddresses(opts.To)
	if err != nil {
		return sendmail.Draft{}, err
	}
	cc, err := parseAddresses(opts.Cc)
	if err != nil {
		return sendmail.Draft{}, err
	}
	bcc, err := parseAddresses(opts.Bcc)
	if err != nil {
		return sendmail.Draft{}, err
	}
	body, err := composeBody(opts.Body, opts.BodyFile)
	if err != nil {
		return sendmail.Draft{}, err
	}
	attachments, err := loadAttachments(opts.Attachments)
	if err != nil {
		return sendmail.Draft{}, err
	}
	return sendmail.Draft{From: mail.Address{Address: named.Email}, To: to, Cc: cc, Bcc: bcc, Subject: opts.Subject, Body: body, Attachments: attachments}, nil
}

func runDraft(rt *Runtime, cmd *cobra.Command, named account.Named, draft sendmail.Draft, execute bool) error {
	raw, err := sendmail.Build(draft)
	if err != nil {
		return &errmap.Error{Kind: errmap.Usage, Message: "邮件内容无法构建", Cause: err}
	}
	summary := sendmail.Summarize(draft, named.SendAllowlist)
	data := map[string]any{"dry_run": !execute, "execute": execute, "sent": false, "summary": summary, "max_messages_per_invocation": sendmail.MaxMessagesPerInvocation, "message_interval_seconds": int(sendmail.DefaultMessageInterval.Seconds())}
	if !execute {
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			printDraftSummary(w, summary)
			_, err := fmt.Fprintln(w, "DRY RUN：未连接 SMTP，未发送邮件。")
			return err
		})
	}
	if err := policy.RequireMutationAllowed(); err != nil {
		return err
	}
	if len(named.SendAllowlist) == 0 {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "收件人白名单为空，拒绝发送", Suggestion: "先在账号配置的 send_allowlist 中加入精确地址或 *@domain"}
	}
	if len(summary.DeniedRecipients) > 0 {
		return &errmap.Error{Kind: errmap.PolicyDenied, Message: "至少一个收件人不在白名单中，拒绝整封邮件", Context: map[string]any{"denied_recipients": summary.DeniedRecipients}}
	}
	printDraftSummary(rt.Err, summary)
	if err := confirmToken(rt, "SEND"); err != nil {
		return err
	}
	authCode, err := sendCredential(rt, named)
	if err != nil {
		return err
	}
	ctx, cancel := rt.context()
	defer cancel()
	store, err := rt.IndexOpen(named.Name, true)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	service := policy.NewMailService(rt.SendMail, store)
	if err := service.Send(ctx, named, authCode, draft, raw, commandName(cmd)); err != nil {
		return err
	}
	data["sent"] = true
	if rt.JSON {
		return writeDetailed(rt, cmd, data, nil, output.Meta{Account: named.Name})
	}
	_, err = fmt.Fprintln(rt.Out, "邮件已提交到 SMTP 服务器。")
	return err
}

func printDraftSummary(w io.Writer, summary sendmail.Summary) {
	_, _ = fmt.Fprintf(w, "From: %s\nTo: %s\nCc: %s\nBcc: %s\nSubject: %s\nBody: %s\n",
		output.SanitizeHuman(summary.From), output.SanitizeHuman(strings.Join(summary.To, ", ")), output.SanitizeHuman(strings.Join(summary.Cc, ", ")), output.SanitizeHuman(strings.Join(summary.Bcc, ", ")), output.SanitizeHuman(summary.Subject), output.SanitizeHuman(summary.BodySummary))
	for _, attachment := range summary.Attachments {
		_, _ = fmt.Fprintf(w, "Attachment: %s (%s, %d bytes)\n", output.SanitizeHuman(attachment.Filename), attachment.ContentType, attachment.SizeBytes)
	}
}

func sendCredential(rt *Runtime, named account.Named) (string, error) {
	provider := rt.Secrets
	if rt.AuthCodeEnv {
		provider = secrets.Environment{}
		_, _ = fmt.Fprintln(rt.Err, "凭证来源：环境变量（仅限本次调用）")
	}
	return provider.Get(named.Email)
}

func composeBody(value, path string) (string, error) {
	if value != "" && path != "" {
		return "", &errmap.Error{Kind: errmap.Usage, Message: "--body 与 --body-file 不能同时使用"}
	}
	if path == "" {
		return value, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxComposeBodyBytes {
		return "", &errmap.Error{Kind: errmap.PolicyDenied, Message: "正文文件超过 1 MiB"}
	}
	raw, err := os.ReadFile(path)
	return string(raw), err
}

func loadAttachments(paths []string) ([]sendmail.Attachment, error) {
	result := make([]sendmail.Attachment, 0, len(paths))
	var total int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, &errmap.Error{Kind: errmap.Usage, Message: "附件必须是普通文件"}
		}
		total += info.Size()
		if total > sendmail.MaxTotalAttachmentBytes {
			return nil, &errmap.Error{Kind: errmap.PolicyDenied, Message: "附件总大小超过 20 MiB"}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		filename := filepath.Base(path)
		result = append(result, sendmail.Attachment{Filename: filename, ContentType: mime.TypeByExtension(filepath.Ext(filename)), Data: raw})
	}
	return result, nil
}

func parseAddresses(values []string) ([]mail.Address, error) {
	result := []mail.Address{}
	for _, value := range values {
		addresses, err := mail.ParseAddressList(value)
		if err != nil {
			return nil, &errmap.Error{Kind: errmap.Usage, Message: "收件人地址格式无效", Cause: err}
		}
		for _, address := range addresses {
			result = append(result, *address)
		}
	}
	return result, nil
}

func loadOriginal(rt *Runtime, id mailmodel.MsgID) (originalMessage, error) {
	ctx, cancel := rt.context()
	defer cancel()
	reader, _, err := rt.connect(ctx)
	if err != nil {
		return originalMessage{}, err
	}
	defer func() { _ = reader.Logout(context.Background()) }()
	raw, truncated, err := reader.FetchBodyPeek(ctx, id, imapx.MaxMessageBytes)
	if err != nil {
		return originalMessage{}, err
	}
	if truncated {
		return originalMessage{}, &errmap.Error{Kind: errmap.ParseError, Message: "原邮件超过 64 MiB，无法安全回复或转发"}
	}
	parsed := mimeparse.Parse(raw)
	if parsed.Parser == "failed" {
		return originalMessage{}, &errmap.Error{Kind: errmap.ParseError, Message: "原邮件 MIME 解析失败"}
	}
	result := originalMessage{Parsed: parsed, References: []string{}}
	message, readErr := mail.ReadMessage(strings.NewReader(string(raw)))
	if readErr == nil {
		if replyTo := message.Header.Get("Reply-To"); replyTo != "" {
			if values, parseErr := mail.ParseAddressList(replyTo); parseErr == nil {
				for _, value := range values {
					result.ReplyTo = append(result.ReplyTo, *value)
				}
			}
		}
		for _, match := range messageIDPattern.FindAllStringSubmatch(message.Header.Get("References"), -1) {
			result.References = append(result.References, match[1])
		}
	}
	if parsed.MessageID != "" {
		result.References = appendUnique(result.References, parsed.MessageID)
	}
	return result, nil
}

func modelAddresses(values []mailmodel.Address) []mail.Address {
	result := make([]mail.Address, 0, len(values))
	for _, value := range values {
		if value.Email != "" {
			result = append(result, mail.Address{Name: value.Name, Address: value.Email})
		}
	}
	return result
}

func prefixedSubject(subject, prefix string) string {
	lower := strings.ToLower(strings.TrimSpace(subject))
	if (prefix == "Re:" && (strings.HasPrefix(lower, "re:") || strings.HasPrefix(lower, "回复:"))) || (prefix == "Fwd:" && (strings.HasPrefix(lower, "fwd:") || strings.HasPrefix(lower, "fw:") || strings.HasPrefix(lower, "转发:"))) {
		return subject
	}
	return prefix + " " + subject
}

func quoteOriginal(parsed mimeparse.Result) string {
	from := strings.Join(addressStringsModel(parsed.From), ", ")
	text := originalText(parsed)
	return fmt.Sprintf("On %s, %s wrote:\n%s", parsed.Date.Format("2006-01-02 15:04 -0700"), from, quoteLines(text))
}

func forwardOriginal(parsed mimeparse.Result) string {
	return fmt.Sprintf("---------- Forwarded message ----------\nFrom: %s\nDate: %s\nSubject: %s\nTo: %s\n\n%s",
		strings.Join(addressStringsModel(parsed.From), ", "), parsed.Date.Format("2006-01-02 15:04 -0700"), parsed.Subject, strings.Join(addressStringsModel(parsed.To), ", "), originalText(parsed))
}

func originalText(parsed mimeparse.Result) string {
	if parsed.Text == nil {
		return "(no text body)"
	}
	value := *parsed.Text
	runes := []rune(value)
	if len(runes) > 8192 {
		value = string(runes[:8192]) + "\n[quoted text truncated]"
	}
	return value
}

func quoteLines(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func addressStringsModel(values []mailmodel.Address) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		address := mail.Address{Name: value.Name, Address: value.Email}
		result = append(result, address.String())
	}
	return result
}

func appendUnique(values []string, value string) []string {
	normalized := strings.Trim(strings.TrimSpace(value), "<>")
	for _, existing := range values {
		if strings.EqualFold(strings.Trim(existing, "<>"), normalized) {
			return values
		}
	}
	return append(values, normalized)
}
