package imapx

import (
	"reflect"
	"testing"

	imap "github.com/emersion/go-imap/v2"
)

// RFC 3501 defines "N:*" as always matching the highest UID in the mailbox
// even when N exceeds it. The window filter is what keeps watch and sync from
// re-announcing the newest message forever.
func TestFilterUIDWindow(t *testing.T) {
	ids := []imap.UID{9, 7, 5}
	got := filterUIDWindow(append([]imap.UID(nil), ids...), SearchFilter{AfterUID: 7})
	if !reflect.DeepEqual(got, []imap.UID{9}) {
		t.Fatalf("AfterUID window: %v", got)
	}
	got = filterUIDWindow(append([]imap.UID(nil), ids...), SearchFilter{AfterUID: 9})
	if len(got) != 0 {
		t.Fatalf("watermark echo survived: %v", got)
	}
	got = filterUIDWindow(append([]imap.UID(nil), ids...), SearchFilter{BeforeUID: 9})
	if !reflect.DeepEqual(got, []imap.UID{7, 5}) {
		t.Fatalf("BeforeUID window: %v", got)
	}
	got = filterUIDWindow(append([]imap.UID(nil), ids...), SearchFilter{})
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("no-window filter changed results: %v", got)
	}
}
