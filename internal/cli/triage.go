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

type triageScopeFlags struct {
	includeCategories []string
	excludeCategories []string
	minConfidence     float64
	minAge            string
	allFolders        bool
}

func (f *triageScopeFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&f.includeCategories, "include-category", nil, "additionally allow a category into the cleanup plan (repeatable)")
	cmd.Flags().StringSliceVar(&f.excludeCategories, "exclude-category", nil, "remove a category from the cleanup plan (repeatable)")
	cmd.Flags().Float64Var(&f.minConfidence, "min-confidence", 0.8, "minimum classification confidence for plan eligibility (0-1)")
	cmd.Flags().StringVar(&f.minAge, "min-age", "30d", "only messages older than this enter the plan (24h, 30d, YYYY-MM-DD; 0 disables)")
	cmd.Flags().BoolVar(&f.allFolders, "all-folders", false, "widen the plan scope beyond the current --folder to every indexed folder")
}

func (f *triageScopeFlags) options(rt *Runtime, now time.Time) (triage.Options, error) {
	if f.minConfidence < 0 || f.minConfidence > 1 {
		return triage.Options{}, &errmap.Error{Kind: errmap.Usage, Message: "--min-confidence 取值范围为 0-1"}
	}
	var olderThan time.Time
	if trimmed := strings.TrimSpace(f.minAge); trimmed != "" && trimmed != "0" {
		parsed, err := parseSince(trimmed, now)
		if err != nil {
			return triage.Options{}, &errmap.Error{Kind: errmap.Usage, Message: "--min-age 只接受 24h、30d、YYYY-MM-DD 或 0", Cause: err}
		}
		olderThan = parsed
	}
	folders := []string{rt.Folder}
	if f.allFolders {
		folders = nil
	}
	return triage.NewOptions(f.includeCategories, f.excludeCategories, f.minConfidence, olderThan, folders), nil
}

func newTriageAnalyzeCommand(rt *Runtime) *cobra.Command {
	var rulesPath string
	scope := &triageScopeFlags{}
	cmd := &cobra.Command{Use: "analyze", Short: "Analyze local message clusters and rule categories", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&rulesPath, "rules", "", "optional TOML rules file")
	scope.register(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		_, analysis, _, err := buildTriage(rt, rulesPath, scope)
		if err != nil {
			return err
		}
		return writeResult(rt, cmd, analysis, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "索引邮件：%d 封，%d 字节；分类 %d 组，发件域 %d 组；按当前范围可进入清理计划：%d 封。\n", analysis.TotalCount, analysis.TotalSizeBytes, len(analysis.ByCategory), len(analysis.ByFromDomain), analysis.PlanEligible)
			return err
		})
	}
	return cmd
}

func newTriagePlanCommand(rt *Runtime) *cobra.Command {
	var outputPath, markdownPath, rulesPath string
	scope := &triageScopeFlags{}
	cmd := &cobra.Command{Use: "plan", Short: "Create a schema-validated cleanup review plan", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&outputPath, "output", "", "required plan.json output path")
	cmd.Flags().StringVar(&markdownPath, "markdown", "", "optional human-review Markdown path")
	cmd.Flags().StringVar(&rulesPath, "rules", "", "optional TOML rules file")
	scope.register(cmd)
	_ = cmd.MarkFlagRequired("output")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := policy.RequireMutationAllowed(); err != nil {
			return err
		}
		plan, analysis, accountName, err := buildTriage(rt, rulesPath, scope)
		if err != nil {
			return err
		}
		if err := cleanupplan.Save(outputPath, plan); err != nil {
			return err
		}
		if markdownPath != "" {
			if err := writePrivateFile(markdownPath, renderPlanMarkdown(plan, analysis)); err != nil {
				return err
			}
		}
		data := map[string]any{"plan_path": outputPath, "markdown_path": markdownPath, "statistics": plan.Statistics, "plan_eligible": analysis.PlanEligible, "indexed_total": analysis.TotalCount, "excluded_by_rule": analysis.ExcludedByRule}
		if rt.JSON {
			return writeDetailed(rt, cmd, data, nil, output.Meta{Account: accountName})
		}
		_, err = fmt.Fprintf(rt.Out, "计划已写入：%s（%d 封进入计划；索引共 %d 封，其余被安全范围排除）。执行前请逐组审阅。\n", outputPath, plan.Statistics.TotalCount, analysis.TotalCount)
		return err
	}
	return cmd
}

func buildTriage(rt *Runtime, rulesPath string, scope *triageScopeFlags) (cleanupplan.Plan, triage.Analysis, string, error) {
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
	now := time.Now()
	opts, err := scope.options(rt, now)
	if err != nil {
		return cleanupplan.Plan{}, triage.Analysis{}, named.Name, err
	}
	plan, analysis := triage.Build(messages, rules, now, opts)
	return plan, analysis, named.Name, nil
}

func renderPlanMarkdown(plan cleanupplan.Plan, analysis triage.Analysis) []byte {
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
	builder.WriteString("> ⚠️ 这份计划里的每一封邮件都会在 `clean --execute` 后被移入服务器已删除文件夹（QQ 会按其回收站周期自动清空）。\n")
	builder.WriteString("> 执行前请逐组审阅；不想清理的条目，直接从 plan.json 的 items 中删除即可。\n")
	builder.WriteString("> 星标邮件、范围外文件夹、未列入清理类别、置信度不足、过新的邮件已被自动排除，不在此列。\n\n")
	_, _ = fmt.Fprintf(&builder, "Plan: %d messages / %d bytes (indexed total: %d; excluded by safety scope: %d)\n\n",
		plan.Statistics.TotalCount, plan.Statistics.TotalSizeBytes, analysis.TotalCount, analysis.TotalCount-analysis.PlanEligible)
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
