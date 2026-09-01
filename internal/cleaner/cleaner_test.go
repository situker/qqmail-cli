package cleaner

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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
	raw           []byte
	uidValidity   uint32
	flags         []string
	sizeDelta     int64
	messageID     string
	examineErr    error
	envelopesErr  error
	envelopesGone bool
	headersErr    error
	peekErr       error
	peekTruncated bool
	peekRaw       []byte
}

func (f fixtureReader) Capabilities() ([]string, []string) { return nil, []string{"UIDPLUS"} }
func (f fixtureReader) ListFolders(context.Context) ([]mailmodel.Folder, error) {
	return []mailmodel.Folder{{Name: "Trash", Attributes: []string{`\Trash`}}}, nil
}
func (f fixtureReader) Examine(context.Context, string) (uint32, uint32, error) {
	if f.examineErr != nil {
		return 0, 0, f.examineErr
	}
	return f.uidValidity, 1, nil
}
func (f fixtureReader) Search(context.Context, imapx.SearchFilter) ([]uint32, error) {
	return []uint32{7}, nil
}
func (f fixtureReader) FetchEnvelopes(context.Context, string, uint32, []uint32) ([]mailmodel.Envelope, error) {
	if f.envelopesErr != nil {
		return nil, f.envelopesErr
	}
	if f.envelopesGone {
		return []mailmodel.Envelope{}, nil
	}
	return []mailmodel.Envelope{{UID: 7, Size: int64(len(f.raw)) + f.sizeDelta, Flags: f.flags}}, nil
}
func (f fixtureReader) FetchHeaderFields(context.Context, []uint32) ([]mailmodel.HeaderFields, error) {
	if f.headersErr != nil {
		return nil, f.headersErr
	}
	messageID := f.messageID
	if messageID == "" {
		messageID = "<fixture@example.com>"
	}
	return []mailmodel.HeaderFields{{UID: 7, MessageID: messageID}}, nil
}
func (f fixtureReader) FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error) {
	return append([]byte(nil), f.raw...), nil
}
func (f fixtureReader) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	if f.peekErr != nil {
		return nil, false, f.peekErr
	}
	body := f.raw
	if f.peekRaw != nil {
		body = f.peekRaw
	}
	return append([]byte(nil), body...), f.peekTruncated, nil
}
func (f fixtureReader) Logout(context.Context) error { return nil }

func gateFixture(t *testing.T) (mailmodel.MsgID, cleanupplan.Plan, account.Named, *secrets.Memory, []byte) {
	t.Helper()
	raw := []byte("From: sender@example.com\r\nSubject: fixture\r\nMessage-ID: <fixture@example.com>\r\n\r\nbody\r\n")
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 11, UID: 7}
	named := account.Named{Name: "personal", Email: "user@qq.com"}
	provider := &secrets.Memory{}
	backupRoot := filepath.Join(t.TempDir(), "backup")
	if _, err := exporter.Export(context.Background(), fixtureReader{raw: raw, uidValidity: 11}, named, []mailmodel.MsgID{id}, backupRoot, provider, "test"); err != nil {
		t.Fatal(err)
	}
	plan := cleanupplan.Plan{Schema: 1, CreatedAt: time.Now(), BackupRoot: backupRoot, Items: []cleanupplan.Item{{ID: id.String()}}}
	return id, plan, named, provider, raw
}

func TestThreeLevelGate(t *testing.T) {
	ctx := context.Background()
	_, plan, named, provider, raw := gateFixture(t)
	result, err := Verify(ctx, fixtureReader{raw: raw, uidValidity: 11}, plan, named, provider, true)
	if err != nil || len(result.Eligible) != 1 || len(result.Failures) != 0 || len(result.AlreadyGone) != 0 {
		t.Fatalf("gate result=%+v err=%v", result, err)
	}
}

func TestGateFailureBranches(t *testing.T) {
	ctx := context.Background()
	_, plan, named, provider, raw := gateFixture(t)
	cases := []struct {
		name       string
		reader     fixtureReader
		paranoid   bool
		wantGate   string
		wantReason string
	}{
		{"uidvalidity_changed", fixtureReader{raw: raw, uidValidity: 12}, false, "server", "UIDVALIDITY changed"},
		{"examine_failed", fixtureReader{raw: raw, uidValidity: 11, examineErr: errors.New("boom")}, false, "server", "folder examination failed"},
		{"fetch_failed", fixtureReader{raw: raw, uidValidity: 11, envelopesErr: errors.New("boom")}, false, "server", "envelope fetch failed"},
		{"size_mismatch", fixtureReader{raw: raw, uidValidity: 11, sizeDelta: 5}, false, "server", "RFC822.SIZE does not match"},
		{"flagged_protected", fixtureReader{raw: raw, uidValidity: 11, flags: []string{`\Flagged`}}, false, "server", "flagged"},
		{"header_fetch_failed", fixtureReader{raw: raw, uidValidity: 11, headersErr: errors.New("boom")}, false, "server", "header fetch failed"},
		{"messageid_mismatch", fixtureReader{raw: raw, uidValidity: 11, messageID: "<other@example.com>"}, false, "server", "Message-ID does not match"},
		{"paranoid_refetch_failed", fixtureReader{raw: raw, uidValidity: 11, peekErr: errors.New("boom")}, true, "paranoid", "refetch failed"},
		{"paranoid_truncated", fixtureReader{raw: raw, uidValidity: 11, peekTruncated: true}, true, "paranoid", "refetch failed or was truncated"},
		{"paranoid_sha_mismatch", fixtureReader{raw: raw, uidValidity: 11, peekRaw: []byte("tampered body")}, true, "paranoid", "SHA-256 does not match"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Verify(ctx, tc.reader, plan, named, provider, tc.paranoid)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Eligible) != 0 || len(result.Failures) != 1 {
				t.Fatalf("result=%+v", result)
			}
			failure := result.Failures[0]
			if failure.Gate != tc.wantGate || !strings.Contains(failure.Reason, tc.wantReason) {
				t.Fatalf("failure=%+v, want gate=%s reason~%q", failure, tc.wantGate, tc.wantReason)
			}
		})
	}
}

func TestGateAlreadyGoneIsNotAFailure(t *testing.T) {
	ctx := context.Background()
	_, plan, named, provider, raw := gateFixture(t)
	result, err := Verify(ctx, fixtureReader{raw: raw, uidValidity: 11, envelopesGone: true}, plan, named, provider, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 0 || len(result.Eligible) != 0 || len(result.AlreadyGone) != 1 {
		t.Fatalf("already-gone handling wrong: %+v", result)
	}
}

func TestGateLocalBranches(t *testing.T) {
	ctx := context.Background()
	id, plan, named, provider, raw := gateFixture(t)
	reader := fixtureReader{raw: raw, uidValidity: 11}
	duplicate := plan
	duplicate.Items = []cleanupplan.Item{{ID: id.String()}, {ID: id.String()}}
	result, err := Verify(ctx, reader, duplicate, named, provider, false)
	if err != nil || len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Reason, "duplicate") {
		t.Fatalf("duplicate branch: %+v err=%v", result, err)
	}
	missing := plan
	missing.Items = []cleanupplan.Item{{ID: mailmodel.MsgID{Folder: "INBOX", UIDValidity: 11, UID: 999}.String()}}
	result, err = Verify(ctx, reader, missing, named, provider, false)
	if err != nil || len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Reason, "absent from verified manifest") {
		t.Fatalf("missing-manifest branch: %+v err=%v", result, err)
	}
}

func TestTrashFolderThreeStates(t *testing.T) {
	ctx := context.Background()
	byAttribute := folderLister{folders: []mailmodel.Folder{{Name: "垃圾箱", Attributes: []string{`\Trash`}}, {Name: "Deleted Messages"}}}
	if name, err := TrashFolder(ctx, byAttribute); err != nil || name != "垃圾箱" {
		t.Fatalf("attribute detection failed: %q err=%v", name, err)
	}
	byName := folderLister{folders: []mailmodel.Folder{{Name: "INBOX"}, {Name: "Deleted Messages"}}}
	if name, err := TrashFolder(ctx, byName); err != nil || name != "Deleted Messages" {
		t.Fatalf("candidate-name fallback failed: %q err=%v", name, err)
	}
	none := folderLister{folders: []mailmodel.Folder{{Name: "INBOX"}}}
	if _, err := TrashFolder(ctx, none); err == nil {
		t.Fatal("missing trash folder did not error")
	}
}

type folderLister struct {
	fixtureReader
	folders []mailmodel.Folder
}

func (f folderLister) ListFolders(context.Context) ([]mailmodel.Folder, error) {
	return f.folders, nil
}
