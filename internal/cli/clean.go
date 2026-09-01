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

const defaultCleanBatchLimit = 500

func newCleanCommand(rt *Runtime) *cobra.Command {
	var planPath string
	var execute, paranoid bool
	var batchLimit int
	cmd := &cobra.Command{Use: "clean", Short: "Move plan-selected messages to the server trash through three safety gates", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&planPath, "plan", "", "required schema-valid and backed-up plan.json")
	cmd.Flags().BoolVar(&execute, "execute", false, "perform the reviewed moves after all gates and TTY confirmation")
	cmd.Flags().BoolVar(&paranoid, "paranoid", false, "refetch every full message and compare SHA-256 before execution")
	cmd.Flags().IntVar(&batchLimit, "batch-limit", defaultCleanBatchLimit, "maximum messages one execution may move; raise explicitly for larger reviewed plans")
	_ = cmd.MarkFlagRequired("plan")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		// The readonly switch is the first check on the write path — before the
		// plan file is even read.
		if execute {
			if err := policy.RequireMutationAllowed(); err != nil {
				return err
			}
		}
		if batchLimit < 1 {
			return &errmap.Error{Kind: errmap.Usage, Message: "--batch-limit 必须大于 0"}
		}
		plan, err := cleanupplan.Load(planPath)
		if err != nil {
			return &errmap.Error{Kind: errmap.ParseError, Message: "清理计划校验失败", Cause: err}
		}
		totalSize, byCategory := planItemStats(plan)
		if !execute {
			data := map[string]any{"dry_run": true, "execute": false, "paranoid": paranoid, "total_count": len(plan.Items), "total_size_bytes": totalSize, "by_category": byCategory, "backup_ready": plan.BackupRoot != ""}
			return writeResult(rt, cmd, data, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "DRY RUN：计划移动 %d 封邮件到服务器已删除文件夹；未执行任何写操作。\n", len(plan.Items))
				return err
			})
		}
		if len(plan.Items) == 0 {
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "清理计划为空，拒绝执行写路径"}
		}
		if len(plan.Items) > batchLimit {
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: fmt.Sprintf("计划包含 %d 封邮件，超过单次执行上限 %d", len(plan.Items), batchLimit), Suggestion: "分批执行，或在人工复核后显式提高 --batch-limit"}
		}
		gateCtx, cancelGate := rt.context()
		mutator, named, err := rt.connectMutator(gateCtx)
		if err != nil {
			cancelGate()
			return err
		}
		defer func() { _ = mutator.Logout(context.Background()) }()
		gate, err := cleaner.Verify(gateCtx, mutator, plan, named, rt.Secrets, paranoid)
		if err != nil {
			cancelGate()
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "备份门禁失败，未执行清理", Cause: err}
		}
		if len(gate.Failures) > 0 || len(gate.Eligible)+len(gate.AlreadyGone) != len(plan.Items) {
			cancelGate()
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "存在未通过三级门禁的邮件，整个清理批次已拒绝", Context: map[string]any{"failures": gate.Failures}}
		}
		alreadyGone := make([]map[string]any, 0, len(gate.AlreadyGone))
		for _, gone := range gate.AlreadyGone {
			alreadyGone = append(alreadyGone, map[string]any{"id": gone.ID, "reason": gone.Reason})
		}
		if len(gate.Eligible) == 0 {
			cancelGate()
			data := map[string]any{"dry_run": false, "execute": true, "paranoid": paranoid, "destination": "", "requested": len(plan.Items), "completed": 0, "results": []map[string]any{}, "already_gone": alreadyGone}
			return writeResult(rt, cmd, data, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "无需清理：计划中的 %d 封邮件都已不在服务器上（本地已有验证过的备份）。\n", len(plan.Items))
				return err
			})
		}
		destination, err := cleaner.TrashFolder(gateCtx, mutator)
		cancelGate()
		if err != nil {
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "无法确定服务器已删除文件夹，拒绝清理", Cause: err}
		}
		printCleanSummary(rt, gate, byCategory, destination, paranoid)
		if err := confirmExactCount(rt, len(gate.Eligible)); err != nil {
			return err
		}
		// The gate context budget was partly consumed by verification and the
		// human's review time; execution gets a fresh budget.
		ctx, cancel := rt.context()
		defer cancel()
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
		data := map[string]any{"dry_run": false, "execute": true, "paranoid": paranoid, "destination": destination, "requested": len(plan.Items), "completed": len(results), "results": results, "already_gone": alreadyGone}
		if rt.JSON {
			return writeDetailed(rt, cmd, data, warnings, output.Meta{Account: named.Name, Skipped: len(warnings)})
		}
		rt.notePartial(warnings)
		_, err = fmt.Fprintf(rt.Out, "清理完成：%d/%d（另有 %d 封已不在服务器）；目标文件夹：%s。\n", len(results), len(gate.Eligible), len(alreadyGone), output.SanitizeLine(destination))
		for _, warning := range warnings {
			_, _ = fmt.Fprintf(rt.Err, "失败 %s：%s\n", warning.ID, output.SanitizeLine(warning.Message))
		}
		return err
	}
	return cmd
}

// planItemStats derives the confirmation statistics from the items themselves.
// plan.Statistics is advisory: an agent may edit items and forget (or decline)
// to update the statistics block, and the human gate must not be misled by it.
func planItemStats(plan cleanupplan.Plan) (int64, map[string]int) {
	var totalSize int64
	byCategory := map[string]int{}
	for _, item := range plan.Items {
		totalSize += item.SizeBytes
		byCategory[item.Category]++
	}
	return totalSize, byCategory
}

func printCleanSummary(rt *Runtime, gate cleaner.GateResult, byCategory map[string]int, destination string, paranoid bool) {
	_, _ = fmt.Fprintf(rt.Err, "将移动 %d 封邮件到 %s（paranoid=%t；另有 %d 封已不在服务器将被跳过）。\n", len(gate.Eligible), output.SanitizeLine(destination), paranoid, len(gate.AlreadyGone))
	keys := make([]string, 0, len(byCategory))
	for category := range byCategory {
		keys = append(keys, category)
	}
	sort.Strings(keys)
	for _, category := range keys {
		_, _ = fmt.Fprintf(rt.Err, "  %s: %d\n", output.SanitizeLine(category), byCategory[category])
	}
}
