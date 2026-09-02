package triage

import (
	"testing"
	"time"

	"github.com/situker/qqmailctl/internal/index"
)

func permissiveOptions() Options {
	return NewOptions(nil, nil, 0, time.Time{}, nil)
}

func TestBuildUsesBuiltinsAndStatistics(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	messages := []index.Message{
		{ID: "one", Folder: "INBOX", Subject: "每周快讯", FromAddr: "news@example.com", ListUnsubscribe: "<mailto:leave@example.com>", InternalDate: now.Add(-8 * 24 * time.Hour), SizeBytes: 20 << 10},
		{ID: "two", Folder: "INBOX", Subject: "您的验证码 123456", FromAddr: "service@example.net", InternalDate: now, SizeBytes: 100, Flags: []string{`\Seen`}},
	}
	plan, analysis := Build(messages, nil, now, permissiveOptions())
	// Only the marketing message may enter the plan: "verification" is not a
	// cleanup-targeted category even under otherwise permissive options.
	if len(plan.Items) != 1 || plan.Statistics.ByCategory["marketing"] != 1 || plan.Statistics.ByCategory["verification"] != 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if analysis.TotalCount != 2 || analysis.PlanEligible != 1 || analysis.ExcludedByRule["category_not_targeted"] != 1 {
		t.Fatalf("unexpected analysis scope accounting: %+v", analysis)
	}
	if analysis.ByFromDomain["example.com"].UnreadRate != 1 || analysis.ByFromDomain["example.net"].UnreadRate != 0 {
		t.Fatalf("unexpected analysis: %+v", analysis)
	}
}

func TestCustomRuleOverridesBuiltinsAndStaysOutOfPlan(t *testing.T) {
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
	message := index.Message{ID: "one", Folder: "INBOX", FromAddr: "news@example.com", ListUnsubscribe: "yes", InternalDate: time.Now()}
	plan, analysis := Build([]index.Message{message}, rules, time.Now(), permissiveOptions())
	// The custom "keep" category overrides the builtin marketing match, and a
	// non-targeted category must never enter the cleanup plan.
	if len(plan.Items) != 0 {
		t.Fatalf("keep-classified message entered the cleanup plan: %+v", plan.Items)
	}
	if analysis.ByCategory["keep"].Count != 1 || analysis.ExcludedByRule["category_not_targeted"] != 1 {
		t.Fatalf("custom rule did not override builtin: %+v", analysis)
	}
}

func TestSafetyScopeExclusions(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-60 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)
	marketing := func(id, folder string, date time.Time, flags []string) index.Message {
		return index.Message{ID: id, Folder: folder, FromAddr: "news@example.com", ListUnsubscribe: "yes", DateHeader: date, InternalDate: date, Flags: flags}
	}
	messages := []index.Message{
		marketing("eligible", "INBOX", old, nil),
		marketing("starred", "INBOX", old, []string{`\Flagged`}),
		marketing("sent-folder", "Sent Messages", old, nil),
		marketing("fresh", "INBOX", recent, nil),
	}
	opts := NewOptions(nil, nil, 0.8, now.Add(-30*24*time.Hour), []string{"INBOX"})
	plan, analysis := Build(messages, nil, now, opts)
	if len(plan.Items) != 1 || plan.Items[0].ID != "eligible" {
		t.Fatalf("safety scope failed: %+v", plan.Items)
	}
	want := map[string]int{"flagged": 1, "folder_out_of_scope": 1, "too_recent": 1}
	for reason, count := range want {
		if analysis.ExcludedByRule[reason] != count {
			t.Fatalf("excluded_by_rule[%s]=%d, want %d (%+v)", reason, analysis.ExcludedByRule[reason], count, analysis.ExcludedByRule)
		}
	}
	// The flagged exclusion has no override: widening every other knob must not
	// let a starred message through.
	wide := NewOptions([]string{"marketing"}, nil, 0, time.Time{}, nil)
	plan, _ = Build([]index.Message{marketing("starred", "INBOX", old, []string{`\Flagged`})}, nil, now, wide)
	if len(plan.Items) != 0 {
		t.Fatal("flagged message entered plan despite protection")
	}
}

// Transactional and government mail must beat the marketing heuristics: a
// wrongly kept newsletter is cheap, a wrongly cleaned receipt is not.
func TestTransactionalAndGovernmentProtection(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	cases := []struct {
		name     string
		message  index.Message
		category string
	}{
		{"apple_receipt_with_unsub", index.Message{ID: "a", Folder: "INBOX", Subject: "你的订单已发货", FromAddr: "order@example-store.com", ListUnsubscribe: "<mailto:x@example.com>", DateHeader: old, InternalDate: old}, "receipt"},
		{"english_receipt_with_unsub", index.Message{ID: "b", Folder: "INBOX", Subject: "Your receipt from Example Store", FromAddr: "billing@example-store.com", ListUnsubscribe: "yes", DateHeader: old, InternalDate: old}, "receipt"},
		{"statement_noreply", index.Message{ID: "c", Folder: "INBOX", Subject: "8 月对账单", FromAddr: "noreply@example-broker.com", DateHeader: old, InternalDate: old}, "receipt"},
		{"gov_notice", index.Message{ID: "d", Folder: "INBOX", Subject: "商标初步审定公告", FromAddr: "notice@sub.gov.cn", DateHeader: old, InternalDate: old}, "official_notice"},
		{"plain_newsletter_still_marketing", index.Message{ID: "e", Folder: "INBOX", Subject: "本周新品速递", FromAddr: "news@example-store.com", ListUnsubscribe: "yes", DateHeader: old, InternalDate: old}, "marketing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, analysis := Build([]index.Message{tc.message}, nil, now, permissiveOptions())
			if analysis.ByCategory[tc.category].Count != 1 {
				t.Fatalf("category=%+v, want %s", analysis.ByCategory, tc.category)
			}
			// Only marketing is cleanup-eligible among these fixtures.
			wantInPlan := tc.category == "marketing"
			if (len(plan.Items) == 1) != wantInPlan {
				t.Fatalf("plan inclusion=%v, want %v (%+v)", len(plan.Items) == 1, wantInPlan, plan.Items)
			}
		})
	}
}

func TestExcludeCategoryNarrowsDefaults(t *testing.T) {
	now := time.Now()
	old := now.Add(-90 * 24 * time.Hour)
	message := index.Message{ID: "m", Folder: "INBOX", FromAddr: "noreply@example.com", DateHeader: old, InternalDate: old}
	opts := NewOptions(nil, []string{"machine_notification"}, 0, time.Time{}, nil)
	plan, analysis := Build([]index.Message{message}, nil, now, opts)
	if len(plan.Items) != 0 || analysis.ExcludedByRule["category_not_targeted"] != 1 {
		t.Fatalf("exclude-category did not narrow plan: %+v %+v", plan.Items, analysis.ExcludedByRule)
	}
}
