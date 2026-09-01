package triage

import (
	"testing"
	"time"

	"github.com/situker/qqmailctl/internal/index"
)

func TestBuildUsesBuiltinsAndStatistics(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	messages := []index.Message{
		{ID: "one", Subject: "每周快讯", FromAddr: "news@example.com", ListUnsubscribe: "<mailto:leave@example.com>", InternalDate: now.Add(-8 * 24 * time.Hour), SizeBytes: 20 << 10},
		{ID: "two", Subject: "您的验证码 123456", FromAddr: "service@example.net", InternalDate: now, SizeBytes: 100, Flags: []string{`\Seen`}},
	}
	plan, analysis := Build(messages, nil, now)
	if len(plan.Items) != 2 || plan.Statistics.ByCategory["marketing"] != 1 || plan.Statistics.ByCategory["verification"] != 1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if analysis.ByFromDomain["example.com"].UnreadRate != 1 || analysis.ByFromDomain["example.net"].UnreadRate != 0 {
		t.Fatalf("unexpected analysis: %+v", analysis)
	}
}

func TestCustomRuleOverridesBuiltins(t *testing.T) {
	rules, err := ParseRules([]byte(`
[[rules]]
name = "trusted-news"
from = "(?i)news@example\\.com"
category = "keep"
confidence = 0.99
reason = "user rule"
`))
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := Build([]index.Message{{ID: "one", FromAddr: "news@example.com", ListUnsubscribe: "yes", InternalDate: time.Now()}}, rules, time.Now())
	if plan.Items[0].Category != "keep" || plan.Items[0].Confidence != .99 {
		t.Fatalf("custom rule did not override builtin: %+v", plan.Items[0])
	}
}
