package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/situker/qqmailctl/internal/cleanupplan"
	"github.com/situker/qqmailctl/internal/errmap"
	"github.com/situker/qqmailctl/internal/output"
	"github.com/situker/qqmailctl/internal/policy"
	"github.com/situker/qqmailctl/internal/triage"
	"github.com/spf13/cobra"
)

func newTriageCommand(rt *Runtime) *cobra.Command {
	root := &cobra.Command{Use: "triage", Short: "Classify indexed messages with deterministic local rules"}
	root.AddCommand(newTriageAnalyzeCommand(rt), newTriagePlanCommand(rt))
	return root
}

func newTriageAnalyzeCommand(rt *Runtime) *cobra.Command {
	var rulesPath string
	cmd := &cobra.Command{Use: "analyze", Short: "Analyze local message clusters and rule categories"}
	cmd.Flags().StringVar(&rulesPath, "rules", "", "optional TOML rules file")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		_, analysis, _, err := buildTriage(rt, rulesPath)
		if err != nil {
			return err
		}
		return writeResult(rt, cmd, analysis, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "索引邮件：%d 封，%d 字节；分类 %d 组，发件域 %d 组。\n", analysis.TotalCount, analysis.TotalSizeBytes, len(analysis.ByCategory), len(analysis.ByFromDomain))
			return err
		})
	}
	return cmd
}

func newTriagePlanCommand(rt *Runtime) *cobra.Command {
	var outputPath, markdownPath, rulesPath string
	cmd := &cobra.Command{Use: "plan", Short: "Create a schema-validated cleanup review plan"}
	cmd.Flags().StringVar(&outputPath, "output", "", "required plan.json output path")
	cmd.Flags().StringVar(&markdownPath, "markdown", "", "optional human-review Markdown path")
	cmd.Flags().StringVar(&rulesPath, "rules", "", "optional TOML rules file")
	_ = cmd.MarkFlagRequired("output")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		plan, _, accountName, err := buildTriage(rt, rulesPath)
		if err != nil {
			return err
		}
		if err := cleanupplan.Save(outputPath, plan); err != nil {
			return err
		}
		if markdownPath != "" {
			if err := writePrivateFile(markdownPath, renderPlanMarkdown(plan)); err != nil {
				return err
			}
		}
		data := map[string]any{"plan_path": outputPath, "markdown_path": markdownPath, "statistics": plan.Statistics}
		if rt.JSON {
			return writeDetailed(rt, cmd, data, nil, output.Meta{Account: accountName})
		}
		_, err = fmt.Fprintf(rt.Out, "计划已写入：%s（%d 封）\n", outputPath, plan.Statistics.TotalCount)
		return err
	}
	return cmd
}

func buildTriage(rt *Runtime, rulesPath string) (cleanupplan.Plan, triage.Analysis, string, error) {
	_, _, named, err := rt.loadAccount()
	if err != nil {
		return cleanupplan.Plan{}, triage.Analysis{}, "", err
	}
	store, err := rt.IndexOpen(named.Name, false)
	if err != nil {
		return cleanupplan.Plan{}, triage.Analysis{}, named.Name, err
	}
	defer func() { _ = store.Close() }()
	ctx, cancel := rt.context()
	defer cancel()
	messages, err := store.Messages(ctx)
	if err != nil {
		return cleanupplan.Plan{}, triage.Analysis{}, named.Name, err
	}
	var raw []byte
	if rulesPath != "" {
		raw, err = os.ReadFile(rulesPath)
		if err != nil {
			return cleanupplan.Plan{}, triage.Analysis{}, named.Name, err
		}
	}
	rules, err := triage.ParseRules(raw)
	if err != nil {
		return cleanupplan.Plan{}, triage.Analysis{}, named.Name, &errmap.Error{Kind: errmap.Usage, Message: "规则文件无效", Cause: err}
	}
	plan, analysis := triage.Build(messages, rules, time.Now())
	return plan, analysis, named.Name, nil
}

func renderPlanMarkdown(plan cleanupplan.Plan) []byte {
	groups := map[string][]cleanupplan.Item{}
	for _, item := range plan.Items {
		from := "(unknown)"
		if len(item.From) > 0 {
			from = item.From[0].Email
			if from == "" {
				from = item.From[0].Name
			}
		}
		groups[from] = append(groups[from], item)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString("# qqmailctl triage review\n\n")
	_, _ = fmt.Fprintf(&builder, "Total: %d messages / %d bytes\n\n", plan.Statistics.TotalCount, plan.Statistics.TotalSizeBytes)
	for _, key := range keys {
		_, _ = fmt.Fprintf(&builder, "## %s\n\n", output.SanitizeMarkdown(key))
		builder.WriteString("| Date | Category | Confidence | Subject | Reason | Evidence |\n|---|---|---:|---|---|---|\n")
		for _, item := range groups[key] {
			_, _ = fmt.Fprintf(&builder, "| %s | %s | %.2f | %s | %s | %s |\n",
				item.Date.Local().Format("2006-01-02"), output.SanitizeMarkdown(item.Category), item.Confidence,
				output.SanitizeMarkdown(item.Subject), output.SanitizeMarkdown(item.Reason), output.SanitizeMarkdown(strings.Join(item.Evidence, "; ")))
		}
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

func writePrivateFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
