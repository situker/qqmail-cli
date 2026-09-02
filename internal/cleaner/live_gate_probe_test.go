package cleaner

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/cleanupplan"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/secrets"
)

// Manual live diagnostic: runs the three-level gate against the configured
// real account for a SAMPLE of a plan and reports failure reasons grouped by
// gate/reason (ids and counts only — no subjects or addresses). Gated on
// QQMAILCTL_LIVE_GATE_PROBE=<plan path>; never runs in CI.
func TestLiveGateProbeSample(t *testing.T) {
	planPath := os.Getenv("QQMAILCTL_LIVE_GATE_PROBE")
	if planPath == "" {
		t.Skip("set QQMAILCTL_LIVE_GATE_PROBE=<plan.json> to run against the configured real account")
	}
	plan, err := cleanupplan.Load(planPath)
	if err != nil {
		t.Fatal(err)
	}
	sample := plan
	if os.Getenv("QQMAILCTL_LIVE_GATE_FULL") != "1" && len(sample.Items) > 40 {
		sample.Items = sample.Items[:40]
	}
	cfg, _, err := account.Load("")
	if err != nil {
		t.Fatal(err)
	}
	named, err := cfg.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	authCode, err := (secrets.Keyring{}).Get(named.Email)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	reader, err := imapx.DialWithVersion(ctx, named, authCode, "gate-probe")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Logout(context.Background()) }()
	result, err := Verify(ctx, reader, sample, named, secrets.Keyring{}, false, nil)
	if err != nil {
		t.Fatalf("gate infrastructure error: %v", err)
	}
	t.Logf("sample=%d eligible=%d failures=%d already_gone=%d", len(sample.Items), len(result.Eligible), len(result.Failures), len(result.AlreadyGone))
	byReason := map[string]int{}
	for _, failure := range result.Failures {
		byReason[failure.Gate+" | "+failure.Reason]++
	}
	for reason, count := range byReason {
		t.Logf("  %3d x %s", count, reason)
	}
	if len(result.Failures) > 0 {
		t.Logf("first failing id: %s", result.Failures[0].ID)
	}
	if out := os.Getenv("QQMAILCTL_LIVE_GATE_ELIGIBLE_OUT"); out != "" {
		ids := make([]string, 0, len(result.Eligible))
		for _, e := range result.Eligible {
			ids = append(ids, e.IDString)
		}
		payload, _ := json.Marshal(ids)
		if err := os.WriteFile(out, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d eligible ids to %s", len(ids), out)
	}
}
