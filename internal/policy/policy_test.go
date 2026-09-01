package policy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

type fakeMutator struct {
	caps      []string
	seenCalls int
	moveCalls int
	copyCalls int
}

func (f *fakeMutator) Capabilities() ([]string, []string)                           { return nil, f.caps }
func (f *fakeMutator) ListFolders(context.Context) ([]mailmodel.Folder, error)      { return nil, nil }
func (f *fakeMutator) Examine(context.Context, string) (uint32, uint32, error)      { return 1, 1, nil }
func (f *fakeMutator) Search(context.Context, imapx.SearchFilter) ([]uint32, error) { return nil, nil }
func (f *fakeMutator) FetchEnvelopes(context.Context, string, uint32, []uint32) ([]mailmodel.Envelope, error) {
	return nil, nil
}
func (f *fakeMutator) FetchHeaderFields(context.Context, []uint32) ([]mailmodel.HeaderFields, error) {
	return nil, nil
}
func (f *fakeMutator) FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error) { return nil, nil }
func (f *fakeMutator) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (f *fakeMutator) Logout(context.Context) error { return nil }
func (f *fakeMutator) SetSeen(context.Context, mailmodel.MsgID) error {
	f.seenCalls++
	return nil
}
func (f *fakeMutator) MoveUID(context.Context, mailmodel.MsgID, string) (imapx.MutationResult, error) {
	f.moveCalls++
	return imapx.MutationResult{Method: "uid_move"}, nil
}
func (f *fakeMutator) CopyMarkDeletedUID(context.Context, mailmodel.MsgID, string, imapx.MessageIdentity) (imapx.MutationResult, error) {
	f.copyCalls++
	return imapx.MutationResult{Method: "copy_store_deleted", SourceRetained: true}, nil
}
func (f *fakeMutator) LocateByIdentity(context.Context, string, imapx.MessageIdentity) ([]mailmodel.MsgID, error) {
	return []mailmodel.MsgID{}, nil
}

func TestReadonlyBlocksBeforeWriter(t *testing.T) {
	t.Setenv(ReadonlyEnv, "1")
	writer := &fakeMutator{}
	service := New(writer, openAuditStore(t))
	err := service.MarkRead(context.Background(), mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}, "message.mark-read", "")
	if err == nil || writer.seenCalls != 0 {
		t.Fatalf("readonly mutation was not blocked: calls=%d err=%v", writer.seenCalls, err)
	}
}

func TestFallbackUsesConservativeCopyAndAudits(t *testing.T) {
	t.Setenv(ReadonlyEnv, "0")
	writer := &fakeMutator{caps: []string{"UIDPLUS"}}
	store := openAuditStore(t)
	service := New(writer, store)
	result, err := service.Move(context.Background(), mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}, "Trash", imapx.MessageIdentity{}, "clean", "plan.json")
	if err != nil || writer.copyCalls != 1 || writer.moveCalls != 0 || !result.SourceRetained {
		t.Fatalf("fallback result=%+v writer=%+v err=%v", result, writer, err)
	}
	entries, err := store.AuditList(context.Background(), 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("audit entries=%+v err=%v", entries, err)
	}
}

func TestMoveUsesNativeMoveWhenAdvertised(t *testing.T) {
	t.Setenv(ReadonlyEnv, "0")
	writer := &fakeMutator{caps: []string{"MOVE", "UIDPLUS"}}
	service := New(writer, openAuditStore(t))
	result, err := service.Move(context.Background(), mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}, "Trash", imapx.MessageIdentity{}, "clean", "plan.json")
	if err != nil || writer.moveCalls != 1 || writer.copyCalls != 0 || result.Method != "uid_move" {
		t.Fatalf("native MOVE path not taken: result=%+v writer=%+v err=%v", result, writer, err)
	}
}

func TestMarkReadPositivePathAudits(t *testing.T) {
	t.Setenv(ReadonlyEnv, "0")
	writer := &fakeMutator{}
	store := openAuditStore(t)
	service := New(writer, store)
	if err := service.MarkRead(context.Background(), mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}, "message.mark-read", ""); err != nil || writer.seenCalls != 1 {
		t.Fatalf("mark-read positive path: calls=%d err=%v", writer.seenCalls, err)
	}
	entries, err := store.AuditList(context.Background(), 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("audit entries=%+v err=%v", entries, err)
	}
}

func openAuditStore(t *testing.T) *index.DB {
	t.Helper()
	store, err := index.OpenPath(filepath.Join(t.TempDir(), "cache.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
