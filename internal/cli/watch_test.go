package cli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/situker/qqmail-cli/internal/imapx"
	"github.com/situker/qqmail-cli/internal/mailmodel"
)

type watchReader struct {
	searchResult []uint32
}

func (w watchReader) Capabilities() ([]string, []string)                      { return nil, nil }
func (w watchReader) ListFolders(context.Context) ([]mailmodel.Folder, error) { return nil, nil }
func (w watchReader) Examine(context.Context, string) (uint32, uint32, error) { return 10, 1, nil }
func (w watchReader) Search(context.Context, imapx.SearchFilter) ([]uint32, error) {
	return w.searchResult, nil
}
func (w watchReader) FetchEnvelopes(_ context.Context, folder string, validity uint32, ids []uint32) ([]mailmodel.Envelope, error) {
	result := []mailmodel.Envelope{}
	for _, uid := range ids {
		result = append(result, mailmodel.Envelope{ID: mailmodel.MsgID{Folder: folder, UIDValidity: validity, UID: uid}.String(), Folder: folder, UID: uid, UIDValidity: validity, From: []mailmodel.Address{}, To: []mailmodel.Address{}, Flags: []string{}})
	}
	return result, nil
}
func (w watchReader) FetchHeaderFields(context.Context, []uint32) ([]mailmodel.HeaderFields, error) {
	return nil, nil
}
func (w watchReader) FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error) { return nil, nil }
func (w watchReader) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (w watchReader) Logout(context.Context) error { return nil }

func TestPollWatchUsesUIDWatermark(t *testing.T) {
	watermark, validity := uint32(4), uint32(10)
	events, err := pollWatch(context.Background(), watchReader{searchResult: []uint32{6, 5}}, "INBOX", &watermark, &validity)
	if err != nil || len(events) != 2 || events[0].Envelope.UID != 5 || watermark != 6 {
		t.Fatalf("events=%+v watermark=%d err=%v", events, watermark, err)
	}
	raw, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	validateOutput(t, "watch.event.schema.json", raw)
}

// RFC 3501: "N:*" always matches the highest UID even with no new mail. A
// server echoing the watermark UID back must produce zero events — otherwise
// watch re-announces the newest message on every poll.
func TestPollWatchDropsWatermarkEcho(t *testing.T) {
	watermark, validity := uint32(6), uint32(10)
	events, err := pollWatch(context.Background(), watchReader{searchResult: []uint32{6}}, "INBOX", &watermark, &validity)
	if err != nil || len(events) != 0 || watermark != 6 {
		t.Fatalf("watermark echo produced events=%+v watermark=%d err=%v", events, watermark, err)
	}
}

func TestPollWatchEmitsFolderResetOnUIDValidityChange(t *testing.T) {
	watermark, validity := uint32(4), uint32(9)
	events, err := pollWatch(context.Background(), watchReader{searchResult: []uint32{6}}, "INBOX", &watermark, &validity)
	if err != nil || len(events) != 1 || events[0].Event != "folder_reset" || events[0].PreviousUIDValidity != 9 {
		t.Fatalf("reset events=%+v err=%v", events, err)
	}
	if validity != 10 || watermark != 6 {
		t.Fatalf("reset did not rebase watermark: validity=%d watermark=%d", validity, watermark)
	}
	raw, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	validateOutput(t, "watch.event.schema.json", raw)
}
