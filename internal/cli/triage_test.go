package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/situker/qqmailctl/internal/cleanupplan"
)

func TestRenderPlanMarkdownSanitizesUntrustedFields(t *testing.T) {
	plan := cleanupplan.Plan{Statistics: cleanupplan.Statistics{TotalCount: 1}, Items: []cleanupplan.Item{{
		Subject: "[run](danger)|\x1b[31mred\u202e", Category: "other", Reason: "fixture", Evidence: []string{"safe"}, Date: time.Now(),
	}}}
	got := string(renderPlanMarkdown(plan))
	if strings.Contains(got, "[run](danger)") || strings.ContainsAny(got, "\x1b\u202e") || strings.Contains(got, "|[run]") {
		t.Fatalf("untrusted Markdown was not neutralized: %q", got)
	}
}
