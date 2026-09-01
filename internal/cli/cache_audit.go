package cli

import (
	"fmt"
	"io"

	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/spf13/cobra"
)

func newCacheCommand(rt *Runtime) *cobra.Command {
	root := &cobra.Command{Use: "cache", Short: "Inspect or completely remove the local index"}
	inspect := &cobra.Command{Use: "inspect", Short: "Inspect cache size and privacy-sensitive row counts"}
	inspect.RunE = func(cmd *cobra.Command, _ []string) error {
		_, _, named, err := rt.loadAccount()
		if err != nil {
			return err
		}
		inspection, err := rt.IndexInspect(named.Name)
		if err != nil {
			return err
		}
		return writeResult(rt, cmd, inspection, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "缓存：%s；存在=%t；邮件=%d；预览=%d；正文=%d（未加密）。\n", inspection.Path, inspection.Exists, inspection.Messages, inspection.PreviewRows, inspection.BodyRows)
			return err
		})
	}
	var execute bool
	clear := &cobra.Command{Use: "clear", Short: "Delete the database, WAL, SHM, and audit JSONL files"}
	clear.Flags().BoolVar(&execute, "execute", false, "perform deletion after TTY confirmation")
	clear.RunE = func(cmd *cobra.Command, _ []string) error {
		_, _, named, err := rt.loadAccount()
		if err != nil {
			return err
		}
		inspection, err := rt.IndexInspect(named.Name)
		if err != nil {
			return err
		}
		if !execute {
			data := map[string]any{"dry_run": true, "execute": false, "path": inspection.Path, "exists": inspection.Exists, "removed": []string{}}
			return writeResult(rt, cmd, data, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "DRY RUN：将删除整个缓存库及旁路文件：%s\n", inspection.Path)
				return err
			})
		}
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		if err := confirmToken(rt, "CLEAR"); err != nil {
			return err
		}
		removed, err := rt.IndexClear(named.Name)
		if err != nil {
			return &errmap.Error{Kind: errmap.Internal, Message: "无法完整删除缓存库", Cause: err}
		}
		if err := rt.AuditAppend(named.Name, index.AuditEntry{Command: commandName(cmd), Action: "cache_clear", Result: "ok"}); err != nil {
			return &errmap.Error{Kind: errmap.Internal, Message: "缓存已删除，但无法写入独立审计 JSONL", Cause: err}
		}
		data := map[string]any{"dry_run": false, "execute": true, "path": inspection.Path, "exists": inspection.Exists, "removed": removed}
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "已删除 %d 个缓存文件。空锁文件不含邮件数据，可能保留。\n", len(removed))
			return err
		})
	}
	root.AddCommand(inspect, clear)
	return root
}

func newAuditCommand(rt *Runtime) *cobra.Command {
	root := &cobra.Command{Use: "audit", Short: "Read local mutation audit records"}
	var limit int
	list := &cobra.Command{Use: "list", Short: "List newest audit records"}
	list.Flags().IntVar(&limit, "limit", 100, "maximum records (1-1000)")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &errmap.Error{Kind: errmap.Usage, Message: "--limit 必须在 1 到 1000 之间"}
		}
		_, _, named, err := rt.loadAccount()
		if err != nil {
			return err
		}
		entries, err := rt.AuditRead(named.Name, limit)
		if err != nil {
			return err
		}
		data := map[string]any{"entries": entries}
		if rt.JSON {
			return writeDetailed(rt, cmd, data, nil, output.Meta{Account: named.Name})
		}
		for _, entry := range entries {
			_, _ = fmt.Fprintf(rt.Out, "%s  %-20s %-20s %s\n", entry.TS.Local().Format("2006-01-02 15:04:05"), entry.Command, entry.Action, entry.Result)
		}
		return nil
	}
	root.AddCommand(list)
	return root
}
