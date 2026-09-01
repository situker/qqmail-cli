package mailmodel

import "testing"

func TestMsgIDRoundTrip(t *testing.T) {
	want := MsgID{Folder: "客户:重要", UIDValidity: 99, UID: 42}
	got, err := ParseMsgID(want.String())
	if err != nil || got != want {
		t.Fatalf("got %#v, %v", got, err)
	}
}
