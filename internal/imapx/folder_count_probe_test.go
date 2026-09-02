package imapx

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/secrets"
)

// Manual live probe: prints current message counts for the mailbox folders
// (counts only — no content). Gated on QQMAIL_CLI_LIVE_PROBE=1.
func TestLiveFolderCounts(t *testing.T) {
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
	authCode, err := (secrets.Keyring{}).Get(named.Email)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := DialWithVersion(ctx, named, authCode, "count-probe")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Logout(context.Background()) }()
	folders, err := client.ListFolders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range folders {
		selectable := true
		for _, attr := range folder.Attributes {
			if attr == `\Noselect` {
				selectable = false
			}
		}
		if !selectable {
			t.Logf("folder %-20q (noselect)", folder.Name)
			continue
		}
		validity, count, err := client.Examine(ctx, folder.Name)
		if err != nil {
			t.Logf("folder %-20q examine error: %v", folder.Name, err)
			continue
		}
		t.Logf("folder %-20q count=%d uidvalidity=%d", folder.Name, count, validity)
	}
}
