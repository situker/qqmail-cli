package cli

import (
	"fmt"
	"io"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/spf13/cobra"
)

func newAccountCommand(rt *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Manage configured accounts"}
	cmd.AddCommand(newAccountListCommand(rt), newAccountUseCommand(rt))
	return cmd
}

func newAccountListCommand(rt *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List configured accounts"}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg, _, err := account.Load(rt.ConfigPath)
		if err != nil {
			return err
		}
		items := cfg.List()
		return writeResult(rt, cmd, map[string]any{"accounts": items, "default_account": cfg.DefaultAccount}, func(w io.Writer) error {
			for _, item := range items {
				marker := " "
				if item.IsDefault {
					marker = "*"
				}
				if _, err := fmt.Fprintf(w, "%s %-16s %-28s %s:%d\n", marker, item.Name, item.Email, item.IMAPHost, item.IMAPPort); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return cmd
}

func newAccountUseCommand(rt *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "use <name>", Args: cobra.ExactArgs(1), Short: "Set the default account"}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, path, err := account.Load(rt.ConfigPath)
		if err != nil {
			return err
		}
		if err := cfg.Use(args[0]); err != nil {
			return err
		}
		if err := cfg.Save(path); err != nil {
			return err
		}
		return writeResult(rt, cmd, map[string]string{"default_account": args[0]}, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "默认账号已切换为 %s。\n", args[0])
			return err
		})
	}
	return cmd
}
