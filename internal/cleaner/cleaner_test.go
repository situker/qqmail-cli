package cleaner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/cleanupplan"
	exporter "github.com/situker/qqmailctl/internal/export"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/secrets"
)

type fixtureReader struct {
	raw         []byte
	uidValidity uint32
}

func (f fixtureReader) Capabilities() ([]string, []string) { return nil, []string{"UIDPLUS"} }
func (f fixtureReader) ListFolders(context.Context) ([]mailmodel.Folder, error) {
	return []mailmodel.Folder{{Name: "Trash", Attributes: []string{`\Trash`}}}, nil
}
func (f fixtureReader) Examine(context.Context, string) (uint32, uint32, error) {
	return f.uidValidity, 1, nil
}
func (f fixtureReader) Search(context.Context, imapx.SearchFilter) ([]uint32, error) {
	return []uint32{7}, nil
}
func (f fixtureReader) FetchEnvelopes(context.Context, string, uint32, []uint32) ([]mailmodel.Envelope, error) {
	return []mailmodel.Envelope{{UID: 7, Size: int64(len(f.raw))}}, nil
}
func (f fixtureReader) FetchHeaderFields(context.Context, []uint32) ([]mailmodel.HeaderFields, error) {
	return []mailmodel.HeaderFields{{UID: 7, MessageID: "<fixture@example.com>"}}, nil
}
func (f fixtureReader) FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error) {
	return append([]byte(nil), f.raw...), nil
}
func (f fixtureReader) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	return append([]byte(nil), f.raw...), false, nil
}
func (f fixtureReader) Logout(context.Context) error { return nil }

func TestThreeLevelGate(t *testing.T) {
	ctx := context.Background()
	raw := []byte("From: sender@example.com\r\nSubject: fixture\r\nMessage-ID: <fixture@example.com>\r\n\r\nbody\r\n")
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 11, UID: 7}
	named := account.Named{Name: "personal", Email: "user@qq.com"}
	provider := &secrets.Memory{}
	backupRoot := filepath.Join(t.TempDir(), "backup")
	if _, err := exporter.Export(ctx, fixtureReader{raw: raw, uidValidity: 11}, named, []mailmodel.MsgID{id}, backupRoot, provider, "test"); err != nil {
		t.Fatal(err)
	}
	plan := cleanupplan.Plan{Schema: 1, CreatedAt: time.Now(), BackupRoot: backupRoot, Items: []cleanupplan.Item{{ID: id.String()}}}
	result, err := Verify(ctx, fixtureReader{raw: raw, uidValidity: 11}, plan, named, provider, true)
	if err != nil || len(result.Eligible) != 1 || len(result.Failures) != 0 {
		t.Fatalf("gate result=%+v err=%v", result, err)
	}
	stale, err := Verify(ctx, fixtureReader{raw: raw, uidValidity: 12}, plan, named, provider, false)
	if err != nil || len(stale.Eligible) != 0 || len(stale.Failures) != 1 || stale.Failures[0].Gate != "server" {
		t.Fatalf("stale result=%+v err=%v", stale, err)
	}
}
