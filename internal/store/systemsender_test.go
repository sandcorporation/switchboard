package store

import (
	"path/filepath"
	"testing"
)

// Mail stored under the system sender's old name is renamed on open, so it
// still counts as system mail (#427).
func TestOpenRenamesOldSystemSender(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, sender := range []string{"agent-mail/system", "agent-mail/system-3", "agent-mail/systemic", "team/worker"} {
		if _, err := db.Exec(`INSERT INTO messages(sender,recipient,subject,body,created_at) VALUES(?,?,?,?,?)`, sender, "team/boss", "s", "b", "2026-11-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT sender FROM messages ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	want := []string{"switchboard/system", "switchboard/system-3", "agent-mail/systemic", "team/worker"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("senders = %v, want %v", got, want)
		}
	}
}
