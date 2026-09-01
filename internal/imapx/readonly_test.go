package imapx

import (
	"reflect"
	"sort"
	"testing"
)

func TestReaderInterfaceIsReadOnlyWhitelist(t *testing.T) {
	typeOf := reflect.TypeOf((*Reader)(nil)).Elem()
	got := make([]string, typeOf.NumMethod())
	for i := 0; i < typeOf.NumMethod(); i++ {
		got[i] = typeOf.Method(i).Name
	}
	sort.Strings(got)
	want := []string{"Capabilities", "Examine", "FetchBodyPeek", "FetchEnvelopes", "FetchMessage", "ListFolders", "Logout", "Search"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reader method surface changed; review readonly guarantee\ngot:  %v\nwant: %v", got, want)
	}
}
