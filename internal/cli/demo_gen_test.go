package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/situker/qqmail-cli/internal/account"
	"github.com/situker/qqmail-cli/internal/imapx"
	"github.com/situker/qqmail-cli/internal/mailmodel"
	"github.com/situker/qqmail-cli/internal/secrets"
)

type demoReader struct {
	fakeReader
	subjects [2]string
}

func (demoReader) Search(context.Context, imapx.SearchFilter) ([]uint32, error) {
	return []uint32{8347, 8346}, nil
}
func (d demoReader) Examine(context.Context, string) (uint32, uint32, error) { return 1425, 2, nil }
func (d demoReader) FetchEnvelopes(_ context.Context, folder string, validity uint32, ids []uint32) ([]mailmodel.Envelope, error) {
	base := time.Date(2026, 8, 30, 9, 12, 0, 0, time.FixedZone("CST", 8*3600))
	result := []mailmodel.Envelope{}
	for i, uid := range ids {
		from := "billing@example.com"
		size := int64(18204)
		if i == 1 {
			from = "team@example.com"
			size = 45012
		}
		result = append(result, mailmodel.Envelope{
			ID: mailmodel.MsgID{Folder: folder, UIDValidity: validity, UID: uid}.String(), UID: uid, UIDValidity: validity,
			Folder: folder, Subject: d.subjects[i], From: []mailmodel.Address{{Email: from}}, To: []mailmodel.Address{{Email: "me@qq.com"}},
			Date: base.Add(time.Duration(-i) * time.Hour), InternalDate: base.Add(time.Duration(-i) * time.Hour), Size: size, Flags: []string{},
		})
	}
	return result, nil
}

// Manual helper: regenerates the README demo block from a real command run so
// the example can never drift from the contract. Skipped unless README_DEMO
// points at an output file.
func TestGenerateReadmeDemoOutput(t *testing.T) {
	target := os.Getenv("README_DEMO")
	if target == "" {
		t.Skip("set README_DEMO=<output file> to regenerate the README demo block")
	}
	var report strings.Builder
	for _, lang := range []struct {
		name     string
		subjects [2]string
	}{
		{"zh", [2]string{"9 月对账单", "周报-第 35 周"}},
		{"en", [2]string{"September statement", "Weekly report - W35"}},
	} {
		var out, stderr bytes.Buffer
		rt := &Runtime{
			Out: &out, Err: &stderr, In: strings.NewReader(""),
			Secrets: &secrets.Memory{Values: map[string]string{"user@qq.com": "abcdefghijklmnop"}},
			Dial: func(context.Context, account.Named, string) (imapx.Reader, error) {
				return demoReader{subjects: lang.subjects}, nil
			},
		}
		dir := t.TempDir()
		configPath := dir + "/config.toml"
		cfg := &account.Config{Schema: account.ConfigSchema, DefaultAccount: "personal", Accounts: map[string]account.Account{"personal": {Email: "user@qq.com"}}}
		if err := cfg.Save(configPath); err != nil {
			t.Fatal(err)
		}
		root := NewRoot(rt)
		root.SetArgs([]string{"--config", configPath, "--json", "envelope", "list", "--unread", "--limit", "2"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		validateOutput(t, "envelope.list.schema.json", out.Bytes())
		var value any
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		pretty, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		report.WriteString("===== " + lang.name + " =====\n")
		report.Write(pretty)
		report.WriteString("\n")
	}
	if err := os.WriteFile(target, []byte(report.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
