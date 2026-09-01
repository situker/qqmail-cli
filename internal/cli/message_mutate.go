package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/spf13/cobra"
)

func newMessageMarkReadCommand(rt *Runtime) *cobra.Command {
	var execute bool
	cmd := &cobra.Command{Use: "mark-read <id>...", Args: cobra.MinimumNArgs(1), Short: "Mark messages read; dry-run unless --execute is confirmed"}
	cmd.Flags().BoolVar(&execute, "execute", false, "perform after TTY count confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ids, err := parseMessageIDs(args)
		if err != nil {
			return err
		}
		if !execute {
			return writeMutationDryRun(rt, cmd, len(ids), map[string]any{"action": "mark_read"})
		}
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(rt.Err, "将把 %d 封邮件标记为已读。\n", len(ids))
		if err := confirmExactCount(rt, len(ids)); err != nil {
			return err
		}
		ctx, cancel := rt.context()
		defer cancel()
		mutator, named, err := rt.connectMutator(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = mutator.Logout(context.Background()) }()
		store, err := rt.IndexOpen(named.Name, true)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		service := policy.New(mutator, store)
		completed := []string{}
		warnings := []output.Warning{}
		for _, id := range ids {
			if err := service.MarkRead(ctx, id, commandName(cmd), ""); err != nil {
				failure, _ := errmap.Details(err)
				warnings = append(warnings, output.Warning{Code: failure.Code, Message: failure.Message, ID: id.String(), Retryable: failure.Retryable})
				continue
			}
			completed = append(completed, id.String())
		}
		return writeMutationResult(rt, cmd, named.Name, len(ids), completed, warnings, map[string]any{"action": "mark_read"})
	}
	return cmd
}

func newMessageMoveCommand(rt *Runtime) *cobra.Command {
	var execute bool
	cmd := &cobra.Command{Use: "move <id>... <folder>", Args: cobra.MinimumNArgs(2), Short: "Move messages; dry-run unless --execute is confirmed"}
	cmd.Flags().BoolVar(&execute, "execute", false, "perform after TTY count confirmation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		destination := args[len(args)-1]
		ids, err := parseMessageIDs(args[:len(args)-1])
		if err != nil {
			return err
		}
		if !execute {
			return writeMutationDryRun(rt, cmd, len(ids), map[string]any{"action": "move", "destination": destination})
		}
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(rt.Err, "将移动 %d 封邮件到 %s。\n", len(ids), output.SanitizeHuman(destination))
		if err := confirmExactCount(rt, len(ids)); err != nil {
			return err
		}
		ctx, cancel := rt.context()
		defer cancel()
		mutator, named, err := rt.connectMutator(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = mutator.Logout(context.Background()) }()
		store, err := rt.IndexOpen(named.Name, true)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		service := policy.New(mutator, store)
		completed := []string{}
		warnings := []output.Warning{}
		results := []imapx.MutationResult{}
		for _, id := range ids {
			identity, identityErr := fetchMessageIdentity(ctx, mutator, id)
			if identityErr != nil {
				failure, _ := errmap.Details(identityErr)
				warnings = append(warnings, output.Warning{Code: failure.Code, Message: failure.Message, ID: id.String(), Retryable: failure.Retryable})
				continue
			}
			result, moveErr := service.Move(ctx, id, destination, identity, commandName(cmd), "")
			if moveErr != nil {
				failure, _ := errmap.Details(moveErr)
				warnings = append(warnings, output.Warning{Code: failure.Code, Message: failure.Message, ID: id.String(), Retryable: failure.Retryable})
				continue
			}
			completed = append(completed, id.String())
			results = append(results, result)
		}
		extra := map[string]any{"action": "move", "destination": destination, "mutation_results": results}
		return writeMutationResult(rt, cmd, named.Name, len(ids), completed, warnings, extra)
	}
	return cmd
}

func parseMessageIDs(values []string) ([]mailmodel.MsgID, error) {
	ids := make([]mailmodel.MsgID, len(values))
	for i, value := range values {
		id, err := mailmodel.ParseMsgID(value)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func fetchMessageIdentity(ctx context.Context, reader imapx.Reader, id mailmodel.MsgID) (imapx.MessageIdentity, error) {
	uidValidity, _, err := reader.Examine(ctx, id.Folder)
	if err != nil {
		return imapx.MessageIdentity{}, err
	}
	if uidValidity != id.UIDValidity {
		return imapx.MessageIdentity{}, &errmap.Error{Kind: errmap.StaleID, Message: "邮件 id 已失效：文件夹 UIDVALIDITY 已变化"}
	}
	envelopes, err := reader.FetchEnvelopes(ctx, id.Folder, uidValidity, []uint32{id.UID})
	if err != nil || len(envelopes) != 1 {
		return imapx.MessageIdentity{}, &errmap.Error{Kind: errmap.NotFound, Message: "邮件不存在", Cause: err}
	}
	headers, err := reader.FetchHeaderFields(ctx, []uint32{id.UID})
	if err != nil || len(headers) != 1 {
		return imapx.MessageIdentity{}, &errmap.Error{Kind: errmap.NotFound, Message: "无法读取邮件 Message-ID", Cause: err}
	}
	return imapx.MessageIdentity{MessageID: headers[0].MessageID, SizeBytes: envelopes[0].Size}, nil
}

func writeMutationDryRun(rt *Runtime, cmd *cobra.Command, count int, extra map[string]any) error {
	data := map[string]any{"dry_run": true, "execute": false, "requested": count, "completed": 0, "ids": []string{}}
	for key, value := range extra {
		data[key] = value
	}
	return writeResult(rt, cmd, data, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "DRY RUN：将处理 %d 封邮件；未执行任何写操作。\n", count)
		return err
	})
}

func writeMutationResult(rt *Runtime, cmd *cobra.Command, accountName string, requested int, completed []string, warnings []output.Warning, extra map[string]any) error {
	data := map[string]any{"dry_run": false, "execute": true, "requested": requested, "completed": len(completed), "ids": completed}
	for key, value := range extra {
		data[key] = value
	}
	if rt.JSON {
		return writeDetailed(rt, cmd, data, warnings, output.Meta{Account: accountName, Skipped: len(warnings)})
	}
	_, err := fmt.Fprintf(rt.Out, "完成 %d/%d。\n", len(completed), requested)
	return err
}
