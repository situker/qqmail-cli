package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/errmap"
	exporter "github.com/situker/qqmailctl/internal/export"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/spf13/cobra"
)

func newExportCommand(rt *Runtime) *cobra.Command {
	var since string
	var idValues []string
	var all, verify bool
	var outputDir string
	var limit int
	cmd := &cobra.Command{Use: "export", Short: "Export raw .eml backups without changing server state"}
	cmd.Flags().StringVar(&since, "since", "", "time window (24h, 7d) or YYYY-MM-DD")
	cmd.Flags().StringSliceVar(&idValues, "ids", nil, "exact message ids")
	cmd.Flags().BoolVar(&all, "all", false, "export the selected folder window")
	cmd.Flags().StringVar(&outputDir, "output", "", "required backup directory")
	cmd.Flags().BoolVar(&verify, "verify", false, "verify an existing local backup without network access")
	cmd.Flags().IntVar(&limit, "limit", 500, "maximum messages for --since/--all (1-500)")
	_ = cmd.MarkFlagRequired("output")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if !verify {
			if err := policy.RequireMutationAllowed(); err != nil {
				return err
			}
		}
		cfg, _, named, err := rt.loadAccount()
		_ = cfg
		if err != nil {
			return err
		}
		if verify {
			if all || since != "" || len(idValues) > 0 {
				return &errmap.Error{Kind: errmap.Usage, Message: "--verify 不能与导出范围参数同时使用"}
			}
			result, err := exporter.Verify(outputDir, named, rt.Secrets)
			if err != nil {
				return &errmap.Error{Kind: errmap.ParseError, Message: "备份校验失败", Context: map[string]any{"manifest": result.ManifestPath, "failures": result.Failures}, Cause: err}
			}
			return writeResult(rt, cmd, result, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "备份校验通过：%d 封邮件。\n", result.Verified)
				return err
			})
		}
		scopeCount := 0
		if all {
			scopeCount++
		}
		if since != "" {
			scopeCount++
		}
		if len(idValues) > 0 {
			scopeCount++
		}
		if scopeCount != 1 {
			return &errmap.Error{Kind: errmap.Usage, Message: "必须且只能指定 --ids、--since 或 --all 中的一种"}
		}
		if limit < 1 || limit > 500 {
			return &errmap.Error{Kind: errmap.Usage, Message: "--limit 必须在 1 到 500 之间"}
		}
		ids := []mailmodel.MsgID{}
		for _, value := range idValues {
			for _, token := range strings.Split(value, ",") {
				id, err := mailmodel.ParseMsgID(strings.TrimSpace(token))
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
		}
		ctx, cancel := rt.context()
		defer cancel()
		reader, connected, err := rt.connect(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Logout(context.Background()) }()
		if all || since != "" {
			sinceTime, err := parseSince(since, time.Now())
			if err != nil {
				return err
			}
			envelopes, _, err := listEnvelopes(ctx, reader, rt.Folder, imapx.SearchFilter{Since: sinceTime, Limit: limit})
			if err != nil {
				return err
			}
			for _, envelope := range envelopes {
				id, err := mailmodel.ParseMsgID(envelope.ID)
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
		}
		result, err := exporter.Export(ctx, reader, account.Named{Name: connected.Name, Email: connected.Email}, ids, outputDir, rt.Secrets, rt.Build.Version)
		if err != nil {
			return err
		}
		warnings := make([]output.Warning, 0, len(result.Failures))
		for _, failure := range result.Failures {
			warnings = append(warnings, output.Warning{Code: "network", Message: failure, Retryable: true})
		}
		if rt.JSON {
			return writeDetailed(rt, cmd, result, warnings, output.Meta{Account: connected.Name, Skipped: result.Skipped})
		}
		_, err = fmt.Fprintf(rt.Out, "导出 %d，跳过 %d；清单：%s\n", result.Exported, result.Skipped, result.ManifestPath)
		return err
	}
	return cmd
}
