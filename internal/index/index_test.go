package index

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCreatesVersionedWALDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	store, err := OpenPath(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	var version int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("user_version=%d, want %d", version, SchemaVersion)
	}
	var mode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode=%q, want WAL", mode)
	}
}

func TestOpenWriteRejectsSecondWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	first, err := OpenPath(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	second, err := OpenPath(path, true)
	if err == nil {
		_ = second.Close()
		t.Fatal("second writer unexpectedly acquired process lock")
	}
}

func TestUIDValidityChangeClearsFolderMessages(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	state, reset, err := store.PrepareFolder(ctx, "INBOX", "/", 10)
	if err != nil || reset {
		t.Fatalf("prepare initial folder: reset=%v err=%v", reset, err)
	}
	if err := store.UpsertMessages(ctx, state.ID, []Message{{
		ID: "one", UID: 3, UIDValidity: 10, InternalDate: time.Now(), Subject: "hello",
	}}); err != nil {
		t.Fatal(err)
	}
	state, reset, err = store.PrepareFolder(ctx, "INBOX", "/", 11)
	if err != nil || !reset || state.LastSeenUID != 0 {
		t.Fatalf("prepare changed folder: state=%+v reset=%v err=%v", state, reset, err)
	}
	messages, err := store.Messages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("message count=%d after UIDVALIDITY reset, want 0", len(messages))
	}
}

func TestSearchUsesChineseBigramsAndLeavesBodiesNullByDefault(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	state, _, err := store.PrepareFolder(ctx, "INBOX", "/", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMessages(ctx, state.ID, []Message{{
		ID: "mail-1", UID: 7, UIDValidity: 10, InternalDate: time.Now(),
		Subject: "项目验收通知", FromAddr: "noreply@example.com",
	}}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(ctx, "项目验收", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "mail-1" {
		t.Fatalf("unexpected Chinese search hits: %+v", hits)
	}
	var preview, body sql.NullString
	if err := store.db.QueryRow("SELECT body_preview, body_text FROM messages WHERE id='mail-1'").Scan(&preview, &body); err != nil {
		t.Fatal(err)
	}
	if preview.Valid || body.Valid {
		t.Fatalf("body fields were persisted by default: preview=%+v body=%+v", preview, body)
	}
}

func TestBigrams(t *testing.T) {
	if got, want := Bigrams("中文 A"), "中文 a"; got != want {
		t.Fatalf("Bigrams=%q, want %q", got, want)
	}
}

func openTestStore(t *testing.T) *DB {
	t.Helper()
	store, err := OpenPath(filepath.Join(t.TempDir(), "cache.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
