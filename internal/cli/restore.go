package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/situker/qqmailctl/internal/cleaner"
	"github.com/situker/qqmailctl/internal/cleanupplan"
	"github.com/situker/qqmailctl/internal/errmap"
	exporter "github.com/situker/qqmailctl/internal/export"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/spf13/cobra"
)

// restore is the deliberate inverse of clean: driven by the same plan and the
// same verified backup manifest, it finds each message in the server trash by
// Message-ID + RFC822.SIZE and moves it back to its original folder. It is the
// regret window for an over-eager cleanup — as long as the QQ trash auto-purge
// cycle has not emptied the copy yet.
func newRestoreCommand(rt *Runtime) *cobra.Command {
	var planPath string
	var execute bool
	cmd := &cobra.Command{Use: "restore", Short: "Move cleaned messages back out of the server trash using the same plan", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&planPath, "plan", "", "required plan.json that was previously backed up and cleaned")
	cmd.Flags().BoolVar(&execute, "execute", false, "perform the restores after TTY count confirmation")
	_ = cmd.MarkFlagRequired("plan")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if execute {
			if err := policy.RequireMutationAllowed(); err != nil {
				return err
			}
		}
		plan, err := cleanupplan.Load(planPath)
		if err != nil {
			return &errmap.Error{Kind: errmap.ParseError, Message: "清理计划校验失败", Cause: err}
		}
		if strings.TrimSpace(plan.BackupRoot) == "" {
			return &errmap.Error{Kind: errmap.Usage, Message: "计划没有 backup_root；restore 依赖 backup 产出的 manifest 提供 Message-ID"}
		}
		if len(plan.Items) == 0 {
			return &errmap.Error{Kind: errmap.Usage, Message: "清理计划为空，没有可还原的邮件"}
		}
		ctx, cancel := rt.context()
		mutator, named, err := rt.connectMutator(ctx)
		if err != nil {
			cancel()
			return err
		}
		defer func() { _ = mutator.Logout(context.Background()) }()
		manifest, _, err := exporter.LoadVerified(plan.BackupRoot, named, rt.Secrets)
		if err != nil {
			cancel()
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "备份 manifest 校验失败，拒绝还原", Cause: err}
		}
		if manifest.Account != named.Name {
			cancel()
			return &errmap.Error{Kind: errmap.PolicyDenied, Message: "manifest 与当前账号不匹配"}
		}
		byID := map[string]exporter.Entry{}
		for _, entry := range manifest.Messages {
			byID[entry.ID] = entry
		}
		trash, err := cleaner.TrashFolder(ctx, mutator)
		if err != nil {
			cancel()
			return &errmap.Error{Kind: errmap.NotFound, Message: "无法确定服务器已删除文件夹", Cause: err}
		}
		type located struct {
			planID   string
			trashID  mailmodel.MsgID
			folder   string
			identity imapx.MessageIdentity
		}
		found := []located{}
		notFound := []map[string]any{}
		warnings := []output.Warning{}
		for _, item := range plan.Items {
			original, parseErr := mailmodel.ParseMsgID(item.ID)
			if parseErr != nil {
				warnings = append(warnings, output.Warning{Code: "usage", Message: "计划条目 id 无法解析", ID: item.ID})
				continue
			}
			entry, ok := byID[item.ID]
			if !ok {
				notFound = append(notFound, map[string]any{"id": item.ID, "reason": "manifest 中没有该邮件的备份记录"})
				continue
			}
			identity := imapx.MessageIdentity{MessageID: entry.MessageID, SizeBytes: entry.Size}
			matches, locateErr := mutator.LocateByIdentity(ctx, trash, identity)
			if locateErr != nil {
				failure, _ := errmap.Details(locateErr)
				warnings = append(warnings, output.Warning{Code: failure.Code, Message: failure.Message, ID: item.ID, Retryable: failure.Retryable})
				continue
			}
			if len(matches) == 0 {
				notFound = append(notFound, map[string]any{"id": item.ID, "reason": "已删除文件夹中找不到该邮件（可能已被服务器回收站周期清空；本地 .eml 备份仍在 backup_root）"})
				continue
			}
			found = append(found, located{planID: item.ID, trashID: matches[0], folder: original.Folder, identity: identity})
		}
		cancel()
		locatedData := make([]map[string]any, 0, len(found))
		for _, item := range found {
			locatedData = append(locatedData, map[string]any{"id": item.planID, "trash_id": item.trashID.String(), "restore_to": item.folder})
		}
		if !execute {
			data := map[string]any{"dry_run": true, "execute": false, "requested": len(plan.Items), "located": locatedData, "not_found": notFound, "trash_folder": trash}
			if rt.JSON {
				return writeDetailed(rt, cmd, data, warnings, output.Meta{Account: named.Name})
			}
			rt.notePartial(warnings)
			_, err = fmt.Fprintf(rt.Out, "DRY RUN：%d/%d 封可从 %s 还原；未执行任何写操作。\n", len(found), len(plan.Items), output.SanitizeLine(trash))
			return err
		}
		if len(found) == 0 {
			data := map[string]any{"dry_run": false, "execute": true, "requested": len(plan.Items), "completed": 0, "results": []map[string]any{}, "not_found": notFound, "trash_folder": trash}
			return writeResult(rt, cmd, data, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "没有可还原的邮件（%d 封在已删除文件夹中未找到）。\n", len(notFound))
				return err
			})
		}
		_, _ = fmt.Fprintf(rt.Err, "将把 %d 封邮件从 %s 还原回原文件夹。\n", len(found), output.SanitizeLine(trash))
		if err := confirmExactCount(rt, len(found)); err != nil {
			return err
		}
		execCtx, cancelExec := rt.context()
		defer cancelExec()
		store, err := rt.IndexOpen(named.Name, true)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		service := policy.New(mutator, store)
		results := []map[string]any{}
		for _, item := range found {
			result, moveErr := service.Move(execCtx, item.trashID, item.folder, item.identity, commandName(cmd), planPath)
			if moveErr != nil {
				failure, _ := errmap.Details(moveErr)
				warnings = append(warnings, output.Warning{Code: failure.Code, Message: failure.Message, ID: item.planID, Retryable: failure.Retryable})
				continue
			}
			results = append(results, map[string]any{"id": item.planID, "restored_to": item.folder, "result": result})
		}
		data := map[string]any{"dry_run": false, "execute": true, "requested": len(plan.Items), "completed": len(results), "results": results, "not_found": notFound, "trash_folder": trash}
		if rt.JSON {
			return writeDetailed(rt, cmd, data, warnings, output.Meta{Account: named.Name, Skipped: len(warnings)})
		}
		rt.notePartial(warnings)
		_, err = fmt.Fprintf(rt.Out, "还原完成：%d/%d。\n", len(results), len(found))
		for _, warning := range warnings {
			_, _ = fmt.Fprintf(rt.Err, "失败 %s：%s\n", warning.ID, output.SanitizeLine(warning.Message))
		}
		return err
	}
	return cmd
}
