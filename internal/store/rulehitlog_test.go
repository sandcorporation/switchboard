package store

import (
	"path/filepath"
	"testing"
)

// A database from before the hit log (#583) keeps its counts, and every hit
// counted so far becomes unlogged: no line stands for it.
func TestOpenMarksOldRuleHitsUnlogged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE rule_hits;
		CREATE TABLE rule_hits(rule_id INTEGER PRIMARY KEY, hits INTEGER NOT NULL DEFAULT 0, last_at TEXT NOT NULL DEFAULT '');
		INSERT INTO rule_hits VALUES(4, 9, '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ { // the second open must not move the count again
		if db, err = Open(path); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err = db.Exec(`UPDATE rule_hits SET hits=hits+1 WHERE rule_id=4`); err != nil {
				t.Fatal(err)
			}
		}
		var hits, unlogged int
		if err = db.QueryRow(`SELECT hits, unlogged FROM rule_hits WHERE rule_id=4`).Scan(&hits, &unlogged); err != nil {
			t.Fatal(err)
		}
		if hits != 10 || unlogged != 9 {
			t.Fatalf("open %d: hits %d unlogged %d", i, hits, unlogged)
		}
		db.Close()
	}
}
