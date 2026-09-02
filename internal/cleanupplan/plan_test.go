package cleanupplan

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/situker/qqmail-cli/internal/mailmodel"
)

func TestSaveLoadValidatesEmbeddedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	want := Plan{Schema: Schema, CreatedAt: time.Now().UTC(), Items: []Item{{
		ID: "m1_fixture", Category: "other", Confidence: .3, Reason: "fixture", Evidence: []string{"fixture"},
		From: []mailmodel.Address{}, Subject: "subject", Date: time.Now().UTC(), SizeBytes: 10,
	}}, Statistics: Statistics{TotalCount: 1, TotalSizeBytes: 10, ByCategory: map[string]int{"other": 1}, ByFromDomain: map[string]int{"example.com": 1}}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Confidence != .3 {
		t.Fatalf("unexpected loaded plan: %+v", got)
	}
}
