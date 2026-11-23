package store

import "database/sql"

// migrateHooks adds the incoming webhooks (#527, internal/hooks), all
// additive: hooks (one registered webhook: its name, receiving role, mode,
// urgency and collapse window and the hash of its address value; its
// original destination is the secret hook:<id>:dest) and hook_receipts
// (each notice received: when, its subject, and — in forward mode — the
// body waiting to be sent on, the tries, the next try and the
// destination's last status code).
func migrateHooks(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS hooks(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		role TEXT NOT NULL,
		mode TEXT NOT NULL,
		urgent INTEGER NOT NULL DEFAULT 0,
		window_min INTEGER NOT NULL DEFAULT 10,
		token_hash TEXT NOT NULL UNIQUE,
		last_at TEXT,
		created_at TEXT NOT NULL,
		created_by TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS hook_receipts(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		hook_id INTEGER NOT NULL,
		at TEXT NOT NULL,
		subject TEXT NOT NULL,
		forward TEXT NOT NULL DEFAULT '',
		body BLOB,
		content_type TEXT NOT NULL DEFAULT '',
		attempts INTEGER NOT NULL DEFAULT 0,
		next_at TEXT,
		status INTEGER NOT NULL DEFAULT 0);
	CREATE INDEX IF NOT EXISTS idx_hook_receipts_hook ON hook_receipts(hook_id, id);
	CREATE INDEX IF NOT EXISTS idx_hook_receipts_due ON hook_receipts(forward, next_at);`)
	return err
}
