package imapx

import (
	"context"
	"os"
	"testing"
	"time"

	imap "github.com/emersion/go-imap/v2"
	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/secrets"
)

// Manual live probe: dumps the SHAPE of QQ's FETCH response for a
// HEADER.FIELDS request (section counts and spec fields only — no message
// content is printed). Gated on QQMAIL_CLI_LIVE_PROBE=1 plus an existing
// keyring credential; never runs in CI.
func TestLiveProbeHeaderFieldsResponseShape(t *testing.T) {
	if os.Getenv("QQMAIL_CLI_LIVE_PROBE") != "1" {
		t.Skip("set QQMAIL_CLI_LIVE_PROBE=1 to run against the configured real account")
	}
	cfg, _, err := account.Load("")
	if err != nil {
		t.Fatal(err)
	}
	named, err := cfg.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	authCode, err := secrets.Keyring{}.Get(named.Email)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := DialWithVersion(ctx, named, authCode, "probe")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Logout(context.Background()) }()
	if _, _, err := client.Examine(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	ids, err := client.Search(ctx, SearchFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatal("no messages")
	}
	for _, variant := range []struct {
		name    string
		section *imap.FetchItemBodySection
	}{
		{"header_fields_subset", &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"From", "To", "Subject", "Date"}, Peek: true}},
		{"full_header", &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}},
	} {
		if err := client.setDeadline(ctx); err != nil {
			t.Fatal(err)
		}
		stop := client.watchdog(ctx)
		items, err := client.raw.Fetch(imap.UIDSetNum(imap.UID(ids[0])), &imap.FetchOptions{UID: true, Flags: true, InternalDate: true, RFC822Size: true, BodySection: []*imap.FetchItemBodySection{variant.section}}).Collect()
		stop()
		if err != nil {
			t.Fatalf("%s: fetch failed: %v", variant.name, err)
		}
		for _, item := range items {
			t.Logf("%s: uid=%d flags=%d size=%d sections=%d", variant.name, item.UID, len(item.Flags), item.RFC822Size, len(item.BodySection))
			for i, part := range item.BodySection {
				spec := part.Section
				t.Logf("  section[%d]: bytes_len=%d spec.Specifier=%q spec.HeaderFields=%v", i, len(part.Bytes), spec.Specifier, spec.HeaderFields)
			}
			fallback := bodySectionBytes(item, variant.section)
			t.Logf("  bodySectionBytes_len=%d", len(fallback))
		}
	}
}
