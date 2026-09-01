package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/imapx"
)

func TestDedicatedQQAccountReadOnly(t *testing.T) {
	if os.Getenv("QQMAILCTL_E2E") != "1" {
		t.Skip("set QQMAILCTL_E2E=1 to run the dedicated-account read-only probe")
	}
	email := os.Getenv("QQMAILCTL_TEST_EMAIL")
	authCode := os.Getenv("QQMAILCTL_AUTH_CODE")
	if email == "" || authCode == "" {
		t.Fatal("QQMAILCTL_TEST_EMAIL and QQMAILCTL_AUTH_CODE are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client, err := imapx.Dial(ctx, account.Named{Name: "e2e", Email: email, IMAPHost: "imap.qq.com", IMAPPort: 993}, authCode)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Logout(context.Background()) }()
	if _, _, err := client.Examine(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	ids, err := client.Search(ctx, imapx.SearchFilter{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) > 5 {
		t.Fatalf("search limit ignored: %d", len(ids))
	}
}
