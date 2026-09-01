package output

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestSuccessEnvelopeIsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, Success("version", map[string]string{"version": "test"}, time.Now())); err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !got.OK || got.SchemaVersion != "1" || got.Command != "version" || got.Error != nil || got.Warnings == nil {
		t.Fatalf("unexpected envelope: %#v", got)
	}
}
