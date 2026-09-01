package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 1

type DB struct {
	db   *sql.DB
	lock *processLock
	path string
}

type FolderState struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Delimiter   string    `json:"delimiter"`
	UIDValidity uint32    `json:"uidvalidity"`
	UIDNext     uint32    `json:"uidnext"`
	LastSyncAt  time.Time `json:"last_sync_at"`
	LastSeenUID uint32    `json:"last_seen_uid"`
}

type Message struct {
	ID              string    `json:"id"`
	FolderID        int64     `json:"folder_id"`
	Folder          string    `json:"folder"`
	UID             uint32    `json:"uid"`
	UIDValidity     uint32    `json:"uidvalidity"`
	Flags           []string  `json:"flags"`
	InternalDate    time.Time `json:"internal_date"`
	SizeBytes       int64     `json:"size_bytes"`
	Subject         string    `json:"subject"`
	FromAddr        string    `json:"from_addr"`
	FromName        string    `json:"from_name"`
	ToAddrs         []string  `json:"to_addrs"`
	DateHeader      time.Time `json:"date_header"`
	MessageID       string    `json:"message_id"`
	ListUnsubscribe string    `json:"list_unsubscribe"`
	Precedence      string    `json:"precedence"`
	HasAttachments  bool      `json:"has_attachments"`
	BodyPreview     *string   `json:"body_preview,omitempty"`
	BodyText        *string   `json:"body_text,omitempty"`
}

type SearchHit struct {
	ID           string    `json:"id"`
	Folder       string    `json:"folder"`
	UID          uint32    `json:"uid"`
	UIDValidity  uint32    `json:"uidvalidity"`
	Subject      string    `json:"subject"`
	FromAddr     string    `json:"from_addr"`
	FromName     string    `json:"from_name"`
	InternalDate time.Time `json:"internal_date"`
	SizeBytes    int64     `json:"size_bytes"`
	Snippet      string    `json:"snippet"`
}

type AuditEntry struct {
	ID      int64     `json:"id"`
	TS      time.Time `json:"ts"`
	Command string    `json:"command"`
	Action  string    `json:"action"`
	MsgID   string    `json:"msg_id,omitempty"`
	Result  string    `json:"result"`
	PlanRef string    `json:"plan_ref,omitempty"`
}

type Inspection struct {
	Path           string `json:"path"`
	Exists         bool   `json:"exists"`
	SizeBytes      int64  `json:"size_bytes"`
	SchemaVersion  int    `json:"schema_version"`
	Folders        int    `json:"folders"`
	Messages       int    `json:"messages"`
	PreviewRows    int    `json:"preview_rows"`
	BodyRows       int    `json:"body_rows"`
	Unencrypted    bool   `json:"unencrypted"`
	PrivacyWarning string `json:"privacy_warning"`
}

func CachePath(accountName string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "qqmailctl", safeAccountName(accountName)+".db"), nil
}

func Open(accountName string, write bool) (*DB, error) {
	path, err := CachePath(accountName)
	if err != nil {
		return nil, err
	}
	return OpenPath(path, write)
}

func OpenPath(path string, write bool) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("cache database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	store := &DB{path: path}
	if write {
		lock, err := acquireProcessLock(path + ".lock")
		if err != nil {
			return nil, err
		}
		store.lock = lock
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		_ = store.lock.close()
		return nil, err
	}
	store.db = db
	db.SetMaxOpenConns(1)
	if err := store.initialize(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (s *DB) initialize(ctx context.Context) error {
	for _, statement := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON"} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > SchemaVersion {
		return fmt.Errorf("cache schema %d is newer than supported schema %d", version, SchemaVersion)
	}
	if version == 0 {
		if err := s.migrateV1(ctx); err != nil {
			return err
		}
		version = 1
	}
	if version != SchemaVersion {
		return fmt.Errorf("cache migration stopped at schema %d", version)
	}
	return nil
}

func (s *DB) migrateV1(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statements := []string{
		`CREATE TABLE folders (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL UNIQUE,
            delimiter TEXT NOT NULL DEFAULT '',
            uidvalidity INTEGER NOT NULL,
            uidnext INTEGER NOT NULL DEFAULT 0,
            last_sync_at TEXT NOT NULL DEFAULT ''
        )`,
		`CREATE TABLE messages (
            id TEXT NOT NULL UNIQUE,
            folder_id INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
            uid INTEGER NOT NULL,
            uidvalidity INTEGER NOT NULL,
            flags TEXT NOT NULL DEFAULT '[]',
            internaldate TEXT NOT NULL,
            size_bytes INTEGER NOT NULL,
            subject TEXT NOT NULL DEFAULT '',
            from_addr TEXT NOT NULL DEFAULT '',
            from_name TEXT NOT NULL DEFAULT '',
            to_addrs TEXT NOT NULL DEFAULT '[]',
            date_header TEXT NOT NULL DEFAULT '',
            message_id TEXT NOT NULL DEFAULT '',
            list_unsubscribe TEXT NOT NULL DEFAULT '',
            precedence TEXT NOT NULL DEFAULT '',
            has_attachments INTEGER NOT NULL DEFAULT 0,
            body_preview TEXT,
            body_text TEXT,
            search_bigrams TEXT NOT NULL DEFAULT '',
            UNIQUE(folder_id, uid)
        )`,
		`CREATE INDEX messages_folder_uid_idx ON messages(folder_id, uid)`,
		`CREATE INDEX messages_internaldate_idx ON messages(internaldate)`,
		`CREATE VIRTUAL TABLE messages_fts USING fts5(
            subject, from_addr, body_preview, body_text, search_bigrams,
            content='messages', content_rowid='rowid'
        )`,
		`CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
            INSERT INTO messages_fts(rowid, subject, from_addr, body_preview, body_text, search_bigrams)
            VALUES (new.rowid, new.subject, new.from_addr, new.body_preview, new.body_text, new.search_bigrams);
        END`,
		`CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
            INSERT INTO messages_fts(messages_fts, rowid, subject, from_addr, body_preview, body_text, search_bigrams)
            VALUES ('delete', old.rowid, old.subject, old.from_addr, old.body_preview, old.body_text, old.search_bigrams);
        END`,
		`CREATE TRIGGER messages_au AFTER UPDATE ON messages BEGIN
            INSERT INTO messages_fts(messages_fts, rowid, subject, from_addr, body_preview, body_text, search_bigrams)
            VALUES ('delete', old.rowid, old.subject, old.from_addr, old.body_preview, old.body_text, old.search_bigrams);
            INSERT INTO messages_fts(rowid, subject, from_addr, body_preview, body_text, search_bigrams)
            VALUES (new.rowid, new.subject, new.from_addr, new.body_preview, new.body_text, new.search_bigrams);
        END`,
		`CREATE TABLE sync_state (
            folder_id INTEGER PRIMARY KEY REFERENCES folders(id) ON DELETE CASCADE,
            last_seen_uid INTEGER NOT NULL DEFAULT 0,
            cursor TEXT NOT NULL DEFAULT ''
        )`,
		`CREATE TABLE audit (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            ts TEXT NOT NULL,
            command TEXT NOT NULL,
            action TEXT NOT NULL,
            msg_id TEXT NOT NULL DEFAULT '',
            result TEXT NOT NULL,
            plan_ref TEXT NOT NULL DEFAULT ''
        )`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DB) Close() error {
	if s == nil {
		return nil
	}
	var dbErr error
	if s.db != nil {
		dbErr = s.db.Close()
	}
	lockErr := s.lock.close()
	if dbErr != nil {
		return dbErr
	}
	return lockErr
}

func (s *DB) Path() string { return s.path }

func (s *DB) PrepareFolder(ctx context.Context, name, delimiter string, uidValidity uint32) (FolderState, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FolderState{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var state FolderState
	var lastSync string
	err = tx.QueryRowContext(ctx, `SELECT f.id, f.name, f.delimiter, f.uidvalidity, f.uidnext, f.last_sync_at,
        COALESCE(s.last_seen_uid, 0) FROM folders f LEFT JOIN sync_state s ON s.folder_id=f.id WHERE f.name=?`, name).
		Scan(&state.ID, &state.Name, &state.Delimiter, &state.UIDValidity, &state.UIDNext, &lastSync, &state.LastSeenUID)
	reset := false
	if errors.Is(err, sql.ErrNoRows) {
		result, insertErr := tx.ExecContext(ctx, "INSERT INTO folders(name, delimiter, uidvalidity) VALUES(?,?,?)", name, delimiter, uidValidity)
		if insertErr != nil {
			return FolderState{}, false, insertErr
		}
		state.ID, _ = result.LastInsertId()
		state.Name, state.Delimiter, state.UIDValidity = name, delimiter, uidValidity
		if _, err := tx.ExecContext(ctx, "INSERT INTO sync_state(folder_id,last_seen_uid,cursor) VALUES(?,0,'')", state.ID); err != nil {
			return FolderState{}, false, err
		}
	} else if err != nil {
		return FolderState{}, false, err
	} else if state.UIDValidity != uidValidity {
		reset = true
		if _, err := tx.ExecContext(ctx, "DELETE FROM messages WHERE folder_id=?", state.ID); err != nil {
			return FolderState{}, false, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE sync_state SET last_seen_uid=0,cursor='' WHERE folder_id=?", state.ID); err != nil {
			return FolderState{}, false, err
		}
		state.LastSeenUID = 0
	}
	if _, err := tx.ExecContext(ctx, "UPDATE folders SET delimiter=?,uidvalidity=? WHERE id=?", delimiter, uidValidity, state.ID); err != nil {
		return FolderState{}, false, err
	}
	state.Delimiter, state.UIDValidity = delimiter, uidValidity
	if lastSync != "" {
		state.LastSyncAt, _ = time.Parse(time.RFC3339Nano, lastSync)
	}
	if err := tx.Commit(); err != nil {
		return FolderState{}, false, err
	}
	return state, reset, nil
}

func (s *DB) UpsertMessages(ctx context.Context, folderID int64, messages []Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statement := `INSERT INTO messages(
        id,folder_id,uid,uidvalidity,flags,internaldate,size_bytes,subject,from_addr,from_name,to_addrs,date_header,
        message_id,list_unsubscribe,precedence,has_attachments,body_preview,body_text,search_bigrams
    ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
    ON CONFLICT(folder_id,uid) DO UPDATE SET
        id=excluded.id,uidvalidity=excluded.uidvalidity,flags=excluded.flags,internaldate=excluded.internaldate,
        size_bytes=excluded.size_bytes,subject=excluded.subject,from_addr=excluded.from_addr,from_name=excluded.from_name,
        to_addrs=excluded.to_addrs,date_header=excluded.date_header,message_id=excluded.message_id,
        list_unsubscribe=excluded.list_unsubscribe,precedence=excluded.precedence,has_attachments=excluded.has_attachments,
		body_preview=COALESCE(excluded.body_preview,messages.body_preview),
		body_text=COALESCE(excluded.body_text,messages.body_text),search_bigrams=excluded.search_bigrams`
	prepared, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		return err
	}
	defer func() { _ = prepared.Close() }()
	maxUID := uint32(0)
	for _, message := range messages {
		flags, _ := json.Marshal(message.Flags)
		toAddrs, _ := json.Marshal(message.ToAddrs)
		searchText := strings.Join([]string{message.Subject, message.FromAddr, valueOrEmpty(message.BodyPreview), valueOrEmpty(message.BodyText)}, " ")
		if _, err := prepared.ExecContext(ctx,
			message.ID, folderID, message.UID, message.UIDValidity, string(flags), formatTime(message.InternalDate), message.SizeBytes,
			message.Subject, message.FromAddr, message.FromName, string(toAddrs), formatTime(message.DateHeader), message.MessageID,
			message.ListUnsubscribe, message.Precedence, boolInt(message.HasAttachments), nullableString(message.BodyPreview), nullableString(message.BodyText), Bigrams(searchText),
		); err != nil {
			return err
		}
		if message.UID > maxUID {
			maxUID = message.UID
		}
	}
	if maxUID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE sync_state SET last_seen_uid=MAX(last_seen_uid,?) WHERE folder_id=?`, maxUID, folderID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE folders SET uidnext=MAX(uidnext,?),last_sync_at=? WHERE id=?`, maxUID+1, formatTime(time.Now().UTC()), folderID); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE folders SET last_sync_at=? WHERE id=?`, formatTime(time.Now().UTC()), folderID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *DB) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("local search query is empty")
	}
	if limit < 1 || limit > 500 {
		return nil, fmt.Errorf("local search limit must be 1 through 500")
	}
	match := quoteFTS(query)
	if containsHan(query) {
		parts := strings.Fields(Bigrams(query))
		quoted := make([]string, len(parts))
		for i, part := range parts {
			quoted[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
		}
		if len(quoted) > 0 {
			match = "search_bigrams:(" + strings.Join(quoted, " AND ") + ")"
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,f.name,m.uid,m.uidvalidity,m.subject,m.from_addr,m.from_name,m.internaldate,m.size_bytes,
		COALESCE(snippet(messages_fts,2,'[',']','…',12),'')
        FROM messages_fts JOIN messages m ON m.rowid=messages_fts.rowid JOIN folders f ON f.id=m.folder_id
        WHERE messages_fts MATCH ? ORDER BY rank LIMIT ?`, match, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	hits := []SearchHit{}
	for rows.Next() {
		var hit SearchHit
		var internalDate string
		if err := rows.Scan(&hit.ID, &hit.Folder, &hit.UID, &hit.UIDValidity, &hit.Subject, &hit.FromAddr, &hit.FromName, &internalDate, &hit.SizeBytes, &hit.Snippet); err != nil {
			return nil, err
		}
		hit.InternalDate, _ = time.Parse(time.RFC3339Nano, internalDate)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func (s *DB) Messages(ctx context.Context) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.folder_id,f.name,m.uid,m.uidvalidity,m.flags,m.internaldate,m.size_bytes,m.subject,
        m.from_addr,m.from_name,m.to_addrs,m.date_header,m.message_id,m.list_unsubscribe,m.precedence,m.has_attachments,m.body_preview,m.body_text
        FROM messages m JOIN folders f ON f.id=m.folder_id ORDER BY m.internaldate DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []Message{}
	for rows.Next() {
		var message Message
		var flags, toAddrs, internalDate, dateHeader string
		var hasAttachments int
		if err := rows.Scan(&message.ID, &message.FolderID, &message.Folder, &message.UID, &message.UIDValidity, &flags, &internalDate,
			&message.SizeBytes, &message.Subject, &message.FromAddr, &message.FromName, &toAddrs, &dateHeader, &message.MessageID,
			&message.ListUnsubscribe, &message.Precedence, &hasAttachments, &message.BodyPreview, &message.BodyText); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(flags), &message.Flags)
		_ = json.Unmarshal([]byte(toAddrs), &message.ToAddrs)
		message.InternalDate, _ = time.Parse(time.RFC3339Nano, internalDate)
		message.DateHeader, _ = time.Parse(time.RFC3339Nano, dateHeader)
		message.HasAttachments = hasAttachments != 0
		result = append(result, message)
	}
	return result, rows.Err()
}

func (s *DB) AddAudit(ctx context.Context, entry AuditEntry) (AuditEntry, error) {
	if entry.TS.IsZero() {
		entry.TS = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, "INSERT INTO audit(ts,command,action,msg_id,result,plan_ref) VALUES(?,?,?,?,?,?)", formatTime(entry.TS), entry.Command, entry.Action, entry.MsgID, entry.Result, entry.PlanRef)
	if err != nil {
		return entry, err
	}
	entry.ID, _ = result.LastInsertId()
	return entry, nil
}

func (s *DB) AuditList(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("audit limit must be 1 through 1000")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,ts,command,action,msg_id,result,plan_ref FROM audit ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	entries := []AuditEntry{}
	for rows.Next() {
		var entry AuditEntry
		var ts string
		if err := rows.Scan(&entry.ID, &ts, &entry.Command, &entry.Action, &entry.MsgID, &entry.Result, &entry.PlanRef); err != nil {
			return nil, err
		}
		entry.TS, _ = time.Parse(time.RFC3339Nano, ts)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *DB) Inspect(ctx context.Context) (Inspection, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		return Inspection{}, err
	}
	inspection := Inspection{Path: s.path, Exists: true, SizeBytes: info.Size(), SchemaVersion: SchemaVersion, Unencrypted: true, PrivacyWarning: "cache is not encrypted; cache clear deletes the database, WAL, and SHM files"}
	for query, target := range map[string]*int{
		"SELECT COUNT(*) FROM folders":                                 &inspection.Folders,
		"SELECT COUNT(*) FROM messages":                                &inspection.Messages,
		"SELECT COUNT(*) FROM messages WHERE body_preview IS NOT NULL": &inspection.PreviewRows,
		"SELECT COUNT(*) FROM messages WHERE body_text IS NOT NULL":    &inspection.BodyRows,
	} {
		if err := s.db.QueryRowContext(ctx, query).Scan(target); err != nil {
			return Inspection{}, err
		}
	}
	return inspection, nil
}

func Clear(accountName string) ([]string, error) {
	path, err := CachePath(accountName)
	if err != nil {
		return nil, err
	}
	lock, err := acquireProcessLock(path + ".lock")
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.close() }()
	removed := []string{}
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err == nil {
			removed = append(removed, candidate)
		} else if !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
	}
	return removed, nil
}

func Bigrams(value string) string {
	runes := []rune(strings.ToLower(value))
	parts := []string{}
	for start := 0; start < len(runes); {
		for start < len(runes) && !unicode.IsLetter(runes[start]) && !unicode.IsDigit(runes[start]) {
			start++
		}
		end := start
		for end < len(runes) && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end])) {
			end++
		}
		word := runes[start:end]
		if len(word) == 1 {
			parts = append(parts, string(word))
		} else {
			for i := 0; i+1 < len(word); i++ {
				parts = append(parts, string(word[i:i+2]))
			}
		}
		start = end
	}
	return strings.Join(parts, " ")
}

func safeAccountName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func quoteFTS(value string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(value), `"`, `""`) + `"`
}

func containsHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func SortedCounts(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
