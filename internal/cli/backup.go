package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/cleanupplan"
	"github.com/situker/qqmailctl/internal/errmap"
	exporter "github.com/situker/qqmailctl/internal/export"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/spf13/cobra"
)

func newBackupCommand(rt *Runtime) *cobra.Command {
	var planPath, outputDir string
	cmd := &cobra.Command{Use: "backup", Short: "Export every message selected by a cleanup plan", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&planPath, "plan", "", "required schema-valid plan.json")
	cmd.Flags().StringVar(&outputDir, "output", "", "required backup directory")
	_ = cmd.MarkFlagRequired("plan")
	_ = cmd.MarkFlagRequired("output")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		plan, err := cleanupplan.Load(planPath)
		if err != nil {
			return &errmap.Error{Kind: errmap.ParseError, Message: "清理计划校验失败", Cause: err}
		}
		ids := make([]mailmodel.MsgID, 0, len(plan.Items))
		for _, item := range plan.Items {
			id, err := mailmodel.ParseMsgID(item.ID)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		ctx, cancel := rt.context()
		defer cancel()
		reader, named, err := rt.connect(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Logout(context.Background()) }()
		result, err := exporter.Export(ctx, reader, account.Named{Name: named.Name, Email: named.Email}, ids, outputDir, rt.Secrets, rt.Build.Version)
		if err != nil {
			return err
		}
		if len(result.Failures) > 0 || result.Exported+result.Skipped != len(ids) {
			return &errmap.Error{Kind: errmap.ParseError, Message: "计划备份不完整，未写入 backup_root", Context: map[string]any{"failures": result.Failures}}
		}
		verified, err := exporter.Verify(outputDir, account.Named{Name: named.Name, Email: named.Email}, rt.Secrets)
		if err != nil {
			return &errmap.Error{Kind: errmap.ParseError, Message: "计划备份校验失败，未写入 backup_root", Cause: err}
		}
		absolute, err := filepath.Abs(outputDir)
		if err != nil {
			return err
		}
		plan.BackupRoot = absolute
		if err := cleanupplan.Save(planPath, plan); err != nil {
			return err
		}
		data := map[string]any{"plan_path": planPath, "backup_root": absolute, "manifest_path": result.ManifestPath, "exported": result.Exported, "skipped": result.Skipped, "verified": verified.Verified, "failures": result.Failures}
		if rt.JSON {
			return writeDetailed(rt, cmd, data, nil, output.Meta{Account: named.Name, Skipped: result.Skipped})
		}
		_, err = fmt.Fprintf(rt.Out, "计划备份完成：导出 %d，跳过 %d，校验 %d；清单：%s\n", result.Exported, result.Skipped, verified.Verified, result.ManifestPath)
		return err
	}
	return cmd
}
