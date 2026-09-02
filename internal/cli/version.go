package cli

import (
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"
)

// Attribution constants travel with every compiled binary: `version` surfaces
// them so the author and license are visible even without the source tree.
const (
	Author     = "司徒K (Situ K)"
	Homepage   = "https://www.situking.com"
	Repository = "https://github.com/situker/qqmail-cli"
	License    = "Apache-2.0"
)

type versionData struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	BuildDate  string `json:"build_date"`
	GoVersion  string `json:"go_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Author     string `json:"author"`
	Homepage   string `json:"homepage"`
	Repository string `json:"repository"`
	License    string `json:"license"`
}

func newVersionCommand(rt *Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "version", Short: "Show build version and attribution", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		data := versionData{
			Version: rt.Build.Version, Commit: rt.Build.Commit, BuildDate: rt.Build.Date,
			GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
			Author: Author, Homepage: Homepage, Repository: Repository, License: License,
		}
		return writeResult(rt, cmd, data, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "qqmail-cli %s (%s)\n作者 %s · %s · %s · %s\n",
				data.Version, data.Commit, data.Author, data.Homepage, data.Repository, data.License)
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
