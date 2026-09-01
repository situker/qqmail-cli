package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/situker/qqmailctl/internal/syncer"
	"github.com/spf13/cobra"
)

func newSyncCommand(rt *Runtime) *cobra.Command {
	var cachePreviews, cacheBodies bool
	cmd := &cobra.Command{Use: "sync", Short: "Incrementally synchronize message metadata into the local index", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&cachePreviews, "cache-previews", false, "store unencrypted text previews (first 2 KiB) in the local cache")
	cmd.Flags().BoolVar(&cacheBodies, "cache-bodies", false, "store unencrypted text bodies in the local cache")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		ctx, cancel := rt.context()
		defer cancel()
		reader, named, err := rt.connect(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Logout(context.Background()) }()
		store, err := rt.IndexOpen(named.Name, true)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		result, err := syncer.Run(ctx, reader, store, syncer.Options{CachePreviews: cachePreviews, CacheBodies: cacheBodies})
		if err != nil {
			return err
		}
		warnings := make([]output.Warning, 0, len(result.Warnings))
		for _, warning := range result.Warnings {
			warnings = append(warnings, output.Warning{Code: "uidvalidity_changed", Message: warning, Retryable: false})
		}
		if rt.JSON {
			return writeDetailed(rt, cmd, result, warnings, output.Meta{Account: named.Name})
		}
		if cachePreviews || cacheBodies {
			_, _ = fmt.Fprintln(rt.Err, "警告：已显式启用未加密的邮件内容缓存；运行 cache clear 可彻底清理。")
		}
		for _, warning := range result.Warnings {
			_, _ = fmt.Fprintln(rt.Err, output.SanitizeHuman(warning))
		}
		_, err = fmt.Fprintf(rt.Out, "同步完成：%d 个文件夹，新增 %d 封；索引：%s\n", len(result.Folders), result.Indexed, store.Path())
		return err
	}
	return cmd
}

func newLocalSearchCommand(rt *Runtime) *cobra.Command {
	var local bool
	var limit int
	cmd := &cobra.Command{Use: "search <query>", Args: cobra.ExactArgs(1), Short: "Search the local FTS5 index"}
	cmd.Flags().BoolVar(&local, "local", false, "required: search the local index without contacting the server")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum hits (1-500)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !local {
			return &errmap.Error{Kind: errmap.Usage, Message: "search 当前只支持 --local"}
		}
		if limit < 1 || limit > 500 {
			return &errmap.Error{Kind: errmap.Usage, Message: "--limit 必须在 1 到 500 之间"}
		}
		_, _, named, err := rt.loadAccount()
		if err != nil {
			return err
		}
		store, err := rt.IndexOpen(named.Name, false)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		ctx, cancel := rt.context()
		defer cancel()
		hits, err := store.Search(ctx, args[0], limit)
		if err != nil {
			return err
		}
		data := map[string]any{"query": args[0], "hits": hits}
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			for _, hit := range hits {
				if _, err := fmt.Fprintf(w, "%s  %-28s  %s\n", hit.InternalDate.Local().Format("2006-01-02 15:04"), output.SanitizeHuman(hit.FromAddr), output.SanitizeHuman(hit.Subject)); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return cmd
}
