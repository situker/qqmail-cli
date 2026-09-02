package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/situker/qqmail-cli/internal/cleanupplan"
	"github.com/situker/qqmail-cli/internal/triage"
)

func TestRenderPlanMarkdownSanitizesUntrustedFields(t *testing.T) {
	plan := cleanupplan.Plan{Statistics: cleanupplan.Statistics{TotalCount: 1}, Items: []cleanupplan.Item{{
		Subject: "[run](danger)|\x1b[31mred\u202e", Category: "other", Reason: "fixture", Evidence: []string{"safe"}, Date: time.Now(),
	}}}
	got := string(renderPlanMarkdown(plan, triage.Analysis{TotalCount: 1, PlanEligible: 1}))
	if strings.Contains(got, "[run](danger)") || strings.ContainsAny(got, "\x1b\u202e") || strings.Contains(got, "|[run]") {
		t.Fatalf("untrusted Markdown was not neutralized: %q", got)
	}
}
