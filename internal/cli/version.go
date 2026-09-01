package cli

import (
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"
)

type versionData struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func newVersionCommand(rt *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "version", Short: "Show build version"}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		data := versionData{Version: rt.Build.Version, Commit: rt.Build.Commit, BuildDate: rt.Build.Date, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "qqmailctl %s (%s)\n", data.Version, data.Commit)
			return err
		})
	}
	return cmd
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: "completion bash|zsh|powershell", Args: cobra.ExactArgs(1), Short: "Generate shell completion"}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return root.GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			return root.GenZshCompletion(cmd.OutOrStdout())
		case "powershell":
			return root.GenPowerShellCompletion(cmd.OutOrStdout())
		default:
			return fmt.Errorf("unsupported shell %q", args[0])
		}
	}
	return cmd
}
