package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/imapx"
	"github.com/situker/qqmailctl/internal/mailmodel"
	"github.com/situker/qqmailctl/internal/secrets"
	projectschemas "github.com/situker/qqmailctl/schemas"
)

// richReader adds realistic list/search results on top of fakeReader so the
// v0.1 read commands produce populated payloads for schema validation.
type richReader struct{ fakeReader }

func (richReader) ListFolders(context.Context) ([]mailmodel.Folder, error) {
	return []mailmodel.Folder{{Name: "INBOX", Delimiter: "/", Attributes: []string{}}}, nil
}
func (richReader) Search(context.Context, imapx.SearchFilter) ([]uint32, error) {
	return []uint32{1}, nil
}
func (richReader) FetchEnvelopes(_ context.Context, folder string, validity uint32, ids []uint32) ([]mailmodel.Envelope, error) {
	result := []mailmodel.Envelope{}
	for _, uid := range ids {
		result = append(result, mailmodel.Envelope{
			ID: mailmodel.MsgID{Folder: folder, UIDValidity: validity, UID: uid}.String(), UID: uid, UIDValidity: validity,
			Folder: folder, Subject: "fixture", From: []mailmodel.Address{{Email: "sender@example.com"}}, To: []mailmodel.Address{}, Flags: []string{}, Size: 90,
		})
	}
	return result, nil
}

// Every v0.1 read command's real --json output must pass its schema — these
// are the highest-traffic agent surfaces.
func TestV01ReadCommandsMatchSchemas(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cfg := &account.Config{Schema: account.ConfigSchema, DefaultAccount: "personal", Accounts: map[string]account.Account{"personal": {Email: "user@qq.com"}}}
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	provider := &secrets.Memory{Values: map[string]string{"user@qq.com": "abcdefghijklmnop"}}
	id := mailmodel.MsgID{Folder: "INBOX", UIDValidity: 1, UID: 1}.String()
	downloadDir := filepath.Join(dir, "downloads")
	exportDir := filepath.Join(dir, "export")
	cases := []struct {
		args   []string
		schema string
	}{
		{[]string{"--config", configPath, "--json", "auth", "status"}, "auth.status.schema.json"},
		{[]string{"--config", configPath, "--json", "account", "list"}, "account.list.schema.json"},
		{[]string{"--config", configPath, "--json", "account", "use", "personal"}, "account.use.schema.json"},
		{[]string{"--config", configPath, "--json", "doctor"}, "doctor.schema.json"},
		{[]string{"--config", configPath, "--json", "folder", "list"}, "folder.list.schema.json"},
		{[]string{"--config", configPath, "--json", "envelope", "list"}, "envelope.list.schema.json"},
		{[]string{"--config", configPath, "--json", "message", "show", id}, "message.show.schema.json"},
		{[]string{"--config", configPath, "--json", "attachment", "list", id}, "attachment.list.schema.json"},
		{[]string{"--config", configPath, "--json", "attachment", "download", id, "all", "--output", downloadDir}, "attachment.download.schema.json"},
		{[]string{"--config", configPath, "--json", "export", "--ids", id, "--output", exportDir}, "export.schema.json"},
		{[]string{"--config", configPath, "--json", "auth", "logout", "--name", "personal"}, "auth.logout.schema.json"},
	}
	for _, tc := range cases {
		var out, stderr bytes.Buffer
		rt := &Runtime{
			Out: &out, Err: &stderr, In: strings.NewReader(""), Secrets: provider,
			Dial: func(context.Context, account.Named, string) (imapx.Reader, error) { return richReader{}, nil },
		}
		root := NewRoot(rt)
		root.SetArgs(tc.args)
		if err := root.Execute(); err != nil {
			t.Fatalf("args %v failed: %v (stderr=%s)", tc.args, err, stderr.String())
		}
		validateOutput(t, tc.schema, out.Bytes())
	}
	// The manifest export just wrote must itself pass the manifest schema.
	manifestRaw, err := os.ReadFile(filepath.Join(exportDir, "personal", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	validateDocument(t, "manifest.schema.json", manifestRaw)
}

// validateDocument checks a standalone JSON document (not an envelope) against
// an embedded schema file.
func validateDocument(t *testing.T, schemaName string, raw []byte) {
	t.Helper()
	document, err := projectschemas.Get(schemaName)
	if err != nil {
		t.Fatal(err)
	}
	var schemaValue, value any
	if err := json.Unmarshal(document, &schemaValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	resource := "https://github.com/situker/qqmailctl/schemas/" + schemaName
	if err := compiler.AddResource(resource, schemaValue); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(value); err != nil {
		t.Fatalf("%s: %v", schemaName, err)
	}
}
