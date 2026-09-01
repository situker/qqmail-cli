package cli

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/situker/qqmailctl/internal/cleaner"
	"github.com/situker/qqmailctl/internal/cleanupplan"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/spf13/cobra"
)

func newCleanCommand(rt *Runtime) *cobra.Command {
	var planPath string
	var execute, paranoid bool
	cmd := &cobra.Command{Use: "clean", Short: "Move plan-selected messages to the server trash through three safety gates"}
	cmd.Flags().StringVar(&planPath, "plan", "", "required schema-valid and backed-up plan.json")
	cmd.Flags().BoolVar(&execute, "execute", false, "perform the reviewed moves after all gates and TTY confirmation")
	cmd.Flags().BoolVar(&paranoid, "paranoid", false, "refetch every full message and compare SHA-256 before execution")
	_ = cmd.MarkFlagRequired("plan")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		plan, err := cleanupplan.Load(planPath)
		if err != nil {
			return &errmap.Error{Kind: errmap.ParseError, Message: "清理计划校验失败", Cause: err}
		}
		if !execute {
			data := map[string]any{"dry_run": true, "execute": false, "paranoid": paranoid, "total_count": len(plan.Items), "total_size_bytes": plan.Statistics.TotalSizeBytes, "by_category": plan.Statistics.ByCategory, "backup_ready": plan.BackupRoot != ""}
			return writeResult(rt, cmd, data, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "DRY RUN：计划移动 %d 封邮件到服务器已删除文件夹；未执行任何写操作。\n", len(plan.Items))
				return err
			})
		}
		if len(plan.Items) == 0 {
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "清理计划为空，拒绝执行写路径"}
		}
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		ctx, cancel := rt.context()
		defer cancel()
		mutator, named, err := rt.connectMutator(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = mutator.Logout(context.Background()) }()
		gate, err := cleaner.Verify(ctx, mutator, plan, named, rt.Secrets, paranoid)
		if err != nil {
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "备份门禁失败，未执行清理", Cause: err}
		}
		if len(gate.Failures) > 0 || len(gate.Eligible) != len(plan.Items) {
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "存在未通过三级门禁的邮件，整个清理批次已拒绝", Context: map[string]any{"failures": gate.Failures}}
		}
		destination, err := cleaner.TrashFolder(ctx, mutator)
		if err != nil {
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "无法确定服务器已删除文件夹，拒绝清理", Cause: err}
		}
		printCleanSummary(rt, plan, destination, paranoid)
		if err := confirmExactCount(rt, len(gate.Eligible)); err != nil {
			return err
		}
		store, err := rt.IndexOpen(named.Name, true)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		service := policy.New(mutator, store)
		results := []map[string]any{}
		warnings := []output.Warning{}
		for _, eligible := range gate.Eligible {
			result, moveErr := service.Move(ctx, eligible.ID, destination, eligible.Identity, commandName(cmd), planPath)
			if moveErr != nil {
				failure, _ := errmap.Details(moveErr)
				warnings = append(warnings, output.Warning{Code: failure.Code, Message: failure.Message, ID: eligible.IDString, Retryable: failure.Retryable})
				continue
			}
			results = append(results, map[string]any{"id": eligible.IDString, "result": result})
		}
		data := map[string]any{"dry_run": false, "execute": true, "paranoid": paranoid, "destination": destination, "requested": len(plan.Items), "completed": len(results), "results": results}
		if rt.JSON {
			return writeDetailed(rt, cmd, data, warnings, output.Meta{Account: named.Name, Skipped: len(warnings)})
		}
		_, err = fmt.Fprintf(rt.Out, "清理完成：%d/%d；目标文件夹：%s。\n", len(results), len(plan.Items), output.SanitizeHuman(destination))
		return err
	}
	return cmd
}

func printCleanSummary(rt *Runtime, plan cleanupplan.Plan, destination string, paranoid bool) {
	_, _ = fmt.Fprintf(rt.Err, "将移动 %d 封邮件到 %s（paranoid=%t）。\n", len(plan.Items), output.SanitizeHuman(destination), paranoid)
	keys := make([]string, 0, len(plan.Statistics.ByCategory))
	for category := range plan.Statistics.ByCategory {
		keys = append(keys, category)
	}
	sort.Strings(keys)
	for _, category := range keys {
		_, _ = fmt.Fprintf(rt.Err, "  %s: %d\n", output.SanitizeHuman(category), plan.Statistics.ByCategory[category])
	}
}
