package cli

import (
	"context"
	"testing"

	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

type searchModeReader struct {
	filters []imapx.SearchFilter
}

func (r *searchModeReader) Capabilities() ([]string, []string) { return nil, nil }
func (r *searchModeReader) ListFolders(context.Context) ([]mailmodel.Folder, error) {
	return nil, nil
}
func (r *searchModeReader) Examine(context.Context, string) (uint32, uint32, error) {
	return 1, 1, nil
}
func (r *searchModeReader) Search(_ context.Context, filter imapx.SearchFilter) ([]uint32, error) {
	r.filters = append(r.filters, filter)
	return []uint32{1}, nil
}
func (r *searchModeReader) FetchEnvelopes(context.Context, string, uint32, []uint32) ([]mailmodel.Envelope, error) {
	return []mailmodel.Envelope{{UID: 1, Subject: "中文测试"}}, nil
}
func (r *searchModeReader) FetchMessage(context.Context, mailmodel.MsgID) ([]byte, error) {
	return nil, nil
}
func (r *searchModeReader) FetchBodyPeek(context.Context, mailmodel.MsgID, int64) ([]byte, bool, error) {
	return nil, false, nil
}
func (r *searchModeReader) Logout(context.Context) error { return nil }

func TestNonASCIISearchUsesClientWindow(t *testing.T) {
	reader := &searchModeReader{}
	items, mode, err := listEnvelopes(context.Background(), reader, "INBOX", imapx.SearchFilter{Subject: "中文", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if mode != "client_window" || len(items) != 1 {
		t.Fatalf("mode=%q items=%#v", mode, items)
	}
	if len(reader.filters) != 1 || reader.filters[0].Subject != "" {
		t.Fatalf("server received unsafe non-ASCII filter: %#v", reader.filters)
	}
}
