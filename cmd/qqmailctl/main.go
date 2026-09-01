package main

import (
	"fmt"
	"os"

	"github.com/situker/qqmailctl/internal/cli"
	"github.com/situker/qqmailctl/internal/output"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	code := 1
	defer func() {
		if recovered := recover(); recovered != nil {
			_, _ = fmt.Fprintln(os.Stderr, output.RedactString(fmt.Sprintf("internal error: %v", recovered)))
			code = 1
		}
		if code != 0 {
			os.Exit(code)
		}
	}()
	code = cli.Execute(cli.BuildInfo{Version: version, Commit: commit, Date: date})
}
