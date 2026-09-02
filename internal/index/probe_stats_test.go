package index

import (
	"context"
	"os"
	"testing"
)

// Manual diagnostic: dumps field-completeness stats for a real index DB.
// Skipped unless INDEX_PROBE points at a database file.
func TestProbeIndexFieldCompleteness(t *testing.T) {
	path := os.Getenv("INDEX_PROBE")
	if path == "" {
		t.Skip("set INDEX_PROBE=<db path> to probe a real index")
	}
	store, err := OpenPath(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	rows, err := store.db.QueryContext(context.Background(), `
		SELECT f.name,
			COUNT(*),
			SUM(CASE WHEN m.from_addr = '' THEN 1 ELSE 0 END),
			SUM(CASE WHEN m.subject = '' THEN 1 ELSE 0 END),
			SUM(CASE WHEN m.message_id = '' THEN 1 ELSE 0 END),
			SUM(CASE WHEN m.list_unsubscribe != '' THEN 1 ELSE 0 END),
			MIN(m.uid), MAX(m.uid)
		FROM messages m JOIN folders f ON f.id = m.folder_id
		GROUP BY f.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	t.Log("folder | total | empty_from | empty_subject | empty_msgid | has_listunsub | uid_min | uid_max")
	for rows.Next() {
		var name string
		var total, emptyFrom, emptySubject, emptyMsgID, hasUnsub, uidMin, uidMax int
		if err := rows.Scan(&name, &total, &emptyFrom, &emptySubject, &emptyMsgID, &hasUnsub, &uidMin, &uidMax); err != nil {
			t.Fatal(err)
		}
		t.Logf("%-20s %6d %6d %6d %6d %6d %8d %8d", name, total, emptyFrom, emptySubject, emptyMsgID, hasUnsub, uidMin, uidMax)
	}
	// Sample: are empty-from rows concentrated in a UID range (= a code era)?
	sample, err := store.db.QueryContext(context.Background(), `
		SELECT f.name, MIN(m.uid), MAX(m.uid), COUNT(*)
		FROM messages m JOIN folders f ON f.id = m.folder_id
		WHERE m.from_addr = '' GROUP BY f.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sample.Close() }()
	t.Log("empty-from ranges:")
	for sample.Next() {
		var name string
		var uidMin, uidMax, count int
		if err := sample.Scan(&name, &uidMin, &uidMax, &count); err != nil {
			t.Fatal(err)
		}
		t.Logf("%-20s uid %d..%d count=%d", name, uidMin, uidMax, count)
	}
}
