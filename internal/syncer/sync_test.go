package syncer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

type fakeReader struct {
	searches []imapx.SearchFilter
}

func (f *fakeReader) Capabilities() ([]string, []string) { return nil, nil }
func (f *fakeReader) ListFolders(context.Context) ([]mailmodel.Folder, error) {
	return []mailmodel.Folder{{Name: "INBOX", Delimiter: "/"}}, nil
}
func (f *fakeReader) Examine(context.Context, string) (uint32, uint32, error) { return 9, 1, nil }
func (f *fakeReader) Search(_ context.Context, filter imapx.SearchFilter) ([]uint32, error) {
	f.searches = append(f.searches, filter)
	return []uint32{3}, nil
}
func (f *fakeReader) FetchEnvelopes(_ context.Context, folder string, validity uint32, ids []uint32) ([]mailmodel.Envelope, error) {
	return []mailmodel.Envelope{{
		ID: mailmodel.MsgID{Folder: folder, UIDValidity: validity, UID: 3}.String(), Folder: folder,
		UID: 3, UIDValidity: validity, Subject: "项目通知", InternalDate: time.Now(), Size: 100,
		From: []mailmodel.Address{{Email: "noreply@example.com"}},
	}}, nil
}
func (f *fakeReader) FetchHeaderFields(context.Context, []uint32) ([]mailmodel.HeaderFields, error) {
	return []mailmodel.HeaderFields{{UID: 3, MessageID: "<three@example.com>", Precedence: "bulk"}}, nil
}
func (f *fakeReader) FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error) { return nil, nil }
func (f *fakeReader) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (f *fakeReader) Logout(context.Context) error { return nil }

func TestRunIndexesHeadersWithoutBodies(t *testing.T) {
	store, err := index.OpenPath(filepath.Join(t.TempDir(), "cache.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	reader := &fakeReader{}
	result, err := Run(context.Background(), reader, store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Indexed != 1 || len(reader.searches) != 2 || reader.searches[0].AfterUID != 0 || reader.searches[1].Limit != recentFlagWindow {
		t.Fatalf("unexpected sync result=%+v searches=%+v", result, reader.searches)
	}
	messages, err := store.Messages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].MessageID != "<three@example.com>" || messages[0].BodyText != nil || messages[0].BodyPreview != nil {
		t.Fatalf("unexpected indexed messages: %+v", messages)
	}
}
