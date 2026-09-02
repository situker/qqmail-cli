package exporter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/mailmodel"
	"github.com/situker/qqmail-cli/internal/secrets"
)

type fakeFetcher struct{ raw []byte }

func (f fakeFetcher) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	return append([]byte(nil), f.raw...), false, nil
}

func TestExportIdempotentAndVerifyTamper(t *testing.T) {
	dir := t.TempDir()
	provider := &secrets.Memory{}
	named := account.Named{Name: "personal", Email: "user@qq.com"}
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 7, UID: 9}
	raw := []byte("From: sender@example.com\r\nSubject: fixture\r\n\r\nbody\r\n")
	first, err := Export(context.Background(), fakeFetcher{raw}, named, []mailmodel.MsgID{id}, dir, provider, "test")
	if err != nil || first.Exported != 1 {
		t.Fatalf("first export: %#v, %v", first, err)
	}
	second, err := Export(context.Background(), fakeFetcher{raw}, named, []mailmodel.MsgID{id}, dir, provider, "test")
	if err != nil || second.Skipped != 1 {
		t.Fatalf("second export: %#v, %v", second, err)
	}
	verified, err := Verify(dir, named, provider)
	if err != nil || verified.Verified != 1 {
		t.Fatalf("verify: %#v, %v", verified, err)
	}
	manifest, err := loadManifest(first.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(first.ManifestPath)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(manifest.Messages[0].Path)), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir, named, provider); err == nil {
		t.Fatal("expected tamper detection")
	}
}
