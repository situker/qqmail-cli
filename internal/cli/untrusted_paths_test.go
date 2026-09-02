package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/imapx"
	projectschemas "github.com/situker/qqmail-cli/schemas"
)

// The schemas are the single source of truth for which fields carry untrusted
// email-originated content. agent-info's untrusted_paths must at least cover
// every property a schema marks UNTRUSTED — a new annotation without a path
// entry fails here.
func TestAgentInfoUntrustedPathsCoverSchemaAnnotations(t *testing.T) {
	annotated := map[string]bool{}
	for _, name := range projectschemas.Names() {
		raw, err := projectschemas.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		collectUntrustedProperties(document, "", annotated)
	}
	if len(annotated) == 0 {
		t.Fatal("no UNTRUSTED annotations found in any schema; the guard itself is broken")
	}
	var out, stderr bytes.Buffer
	rt := &Runtime{
		Out: &out, Err: &stderr, In: strings.NewReader(""),
		Dial: func(context.Context, account.Named, string) (imapx.Reader, error) { return fakeReader{}, nil },
	}
	root := NewRoot(rt)
	root.SetArgs([]string{"agent-info"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			UntrustedPaths []string `json:"untrusted_paths"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(envelope.Data.UntrustedPaths, "\n")
	for property := range annotated {
		if !strings.Contains(joined, property) {
			t.Errorf("schema-annotated untrusted property %q missing from agent-info untrusted_paths", property)
		}
	}
}

// collectUntrustedProperties walks a decoded JSON Schema and records every
// property name whose description begins with "UNTRUSTED".
func collectUntrustedProperties(node any, key string, into map[string]bool) {
	switch value := node.(type) {
	case map[string]any:
		if description, ok := value["description"].(string); ok && strings.HasPrefix(description, "UNTRUSTED") && key != "" {
			into[key] = true
		}
		for childKey, child := range value {
			if childKey == "properties" {
				if properties, ok := child.(map[string]any); ok {
					for propertyName, property := range properties {
						collectUntrustedProperties(property, propertyName, into)
					}
					continue
				}
			}
			collectUntrustedProperties(child, key, into)
		}
	case []any:
		for _, item := range value {
			collectUntrustedProperties(item, key, into)
		}
	}
}
