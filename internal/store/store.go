// Package store opens the SQLite database and owns its schema: every table,
// the additive migrations that upgrade older databases in place, and the
// transaction helper.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sandcorporation/switchboard/internal/ownerfile"
)

// DefaultQuestionTarget is the question_settings row every database starts with.
const DefaultQuestionTarget = "default"

// Open opens and upgrades the SQLite database at path, as the server does.
func Open(path string) (*sql.DB, error) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, err
	}
	if err = Init(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Init creates the tables when missing and upgrades databases created by
// older versions in place. It is safe to run on every start.
func Init(db *sql.DB) error {
	if _, e := db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS messages(id INTEGER PRIMARY KEY AUTOINCREMENT,sender TEXT NOT NULL,recipient TEXT NOT NULL,subject TEXT NOT NULL,body TEXT NOT NULL,created_at TEXT NOT NULL,task_id TEXT,thread_id INTEGER,reply_to INTEGER,status TEXT NOT NULL DEFAULT 'pending',idem TEXT,priority TEXT NOT NULL DEFAULT 'normal', UNIQUE(sender,idem)); CREATE INDEX IF NOT EXISTS idx_recipient ON messages(recipient,id); CREATE INDEX IF NOT EXISTS idx_sender ON messages(sender,id); CREATE INDEX IF NOT EXISTS idx_thread ON messages(thread_id); CREATE INDEX IF NOT EXISTS idx_task ON messages(task_id); CREATE TABLE IF NOT EXISTS role_states(role TEXT NOT NULL,message_id INTEGER NOT NULL,status TEXT NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(role,message_id)); CREATE TABLE IF NOT EXISTS roles(role TEXT PRIMARY KEY, token TEXT NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS slots(base TEXT NOT NULL,number INTEGER NOT NULL,machine TEXT NOT NULL,directory TEXT NOT NULL,session_id TEXT NOT NULL DEFAULT '',label TEXT NOT NULL DEFAULT '',parent TEXT NOT NULL DEFAULT '',registered_at INTEGER NOT NULL,lease_until INTEGER NOT NULL,PRIMARY KEY(base,number),UNIQUE(base,machine,directory));`); e != nil {
		return e
	}
	has, e := HasColumn(db, "messages", "priority")
	if e != nil {
		return e
	}
	if !has {
		// Databases from before message priority: existing mail becomes normal.
		if _, e = db.Exec(`ALTER TABLE messages ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal'`); e != nil {
			return e
		}
	}
	if e = migrateLabels(db); e != nil {
		return e
	}
	if e = migrateDirective(db); e != nil {
		return e
	}
	if e = migratePresence(db); e != nil {
		return e
	}
	if e = migratePresenceLog(db); e != nil {
		return e
	}
	if e = migrateAlerts(db); e != nil {
		return e
	}
	// Before the hash migration, so a backup it writes on this start (after a
	// rollback to a binary from before hashing) survives until the next one.
	if e = dropPlainBackup(db); e != nil {
		return e
	}
	if e = migrateRoleTokens(db); e != nil {
		return e
	}
	if e = migratePermissions(db); e != nil {
		return e
	}
	if e = migrateQuestionSettings(db); e != nil {
		return e
	}
	if e = migrateGroups(db); e != nil {
		return e
	}
	if e = migrateAdminAuth(db); e != nil {
		return e
	}
	if e = migrateInvites(db); e != nil {
		return e
	}
	if e = migrateQuestions(db); e != nil {
		return e
	}
	if e = migrateHandoffs(db); e != nil {
		return e
	}
	if e = migrateQuestionRelay(db); e != nil {
		return e
	}
	if e = migrateRoleAliases(db); e != nil {
		return e
	}
	if e = migrateHosting(db); e != nil {
		return e
	}
	if e = migrateTerminalLog(db); e != nil {
		return e
	}
	if e = migrateTerminalLogPeer(db); e != nil {
		return e
	}
	if e = migrateHostingLimit(db); e != nil {
		return e
	}
	if e = migrateHostingPermissions(db); e != nil {
		return e
	}
	if e = migrateRemind(db); e != nil {
		return e
	}
	if e = migrateAnsweredRule(db); e != nil {
		return e
	}
	if e = migrateQuestionRefusals(db); e != nil {
		return e
	}
	if e = migrateHostedEvents(db); e != nil {
		return e
	}
	if e = migrateAdminActions(db); e != nil {
		return e
	}
	if e = migrateSystemSender(db); e != nil {
		return e
	}
	if e = migrateMailboxViews(db); e != nil {
		return e
	}
	if e = migrateBossWrites(db); e != nil {
		return e
	}
	if e = migrateLedgerViews(db); e != nil {
		return e
	}
	if e = migrateReports(db); e != nil {
		return e
	}
	if e = migrateJobs(db); e != nil {
		return e
	}
	if e = migrateSchedules(db); e != nil {
		return e
	}
	if e = migrateSchedulePauses(db); e != nil {
		return e
	}
	if e = migrateRoleCredentials(db); e != nil {
		return e
	}
	if e = migrateHostAutos(db); e != nil {
		return e
	}
	if e = migrateHostAutoStops(db); e != nil {
		return e
	}
	if e = migrateHostAutoChecks(db); e != nil {
		return e
	}
	if e = migrateHostAutoAlerts(db); e != nil {
		return e
	}
	if e = migrateHostGrants(db); e != nil {
		return e
	}
	if e = migrateRules(db); e != nil {
		return e
	}
	if e = migrateRuleHits(db); e != nil {
		return e
	}
	if e = migrateHostingAdapters(db); e != nil {
		return e
	}
	if e = migrateWorkGroups(db); e != nil {
		return e
	}
	if e = migrateRuleProposals(db); e != nil {
		return e
	}
	if e = migrateLeftItems(db); e != nil {
		return e
	}
	if e = migrateRoleLifecycle(db); e != nil {
		return e
	}
	if e = migrateRequests(db); e != nil {
		return e
	}
	if e = migrateAlertMail(db); e != nil {
		return e
	}
	if e = migrateSecrets(db); e != nil {
		return e
	}
	return migrateHourlyIndexes(db)
}

// migrateAlertMail adds the collapsing alert mail's columns (#525):
// messages.alert_source (hook:<name>, watch:<name>; null for other mail),
// alert_repeats (how many more times the same alert came inside its window)
// and alert_last_at (the last of them, RFC 3339).
func migrateAlertMail(db *sql.DB) error {
	if err := addColumns(db, []columnAdd{
		{"messages", "alert_source", `TEXT`},
		{"messages", "alert_repeats", `INTEGER NOT NULL DEFAULT 0`},
		{"messages", "alert_last_at", `TEXT`},
	}); err != nil {
		return err
	}
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_alert ON messages(recipient, alert_source, subject, id) WHERE alert_source IS NOT NULL`)
	return err
}

// migrateSecrets creates the sealed secret values (#525): each row's value
// is AEAD-sealed under the server's secrets key with its own nonce and its
// name as associated data (internal/secrets). hint is a short trace of the
// value for display, possibly empty.
func migrateSecrets(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS secrets(
		name TEXT PRIMARY KEY,
		nonce BLOB NOT NULL,
		sealed BLOB NOT NULL,
		hint TEXT NOT NULL DEFAULT '',
		set_at TEXT NOT NULL)`)
	return err
}

// migrateHourlyIndexes adds the created_at indexes the hourly counts read
// (#511, insight.Hourly): the last 24 hours of mail and reports.
func migrateHourlyIndexes(db *sql.DB) error {
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
	CREATE INDEX IF NOT EXISTS idx_reports_created ON reports(created_at);`)
	return err
}

// migrateRequests adds the owner's requests (#501, internal/requests), all
// additive: requests (one request the owner gave a boss role: its short
// title and full text, the boss role's result line and when it marked the
// request done, and when a dashboard login checked it), request_notes (the
// owner's later words on a request) and request_id on reports, messages and
// questions (the request a report, a sent mail or a panel question belongs
// to; null for everything before or outside a request). Times are RFC 3339.
func migrateRequests(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS requests(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		author TEXT NOT NULL,
		title TEXT NOT NULL,
		text TEXT NOT NULL,
		created_at TEXT NOT NULL,
		result TEXT NOT NULL DEFAULT '',
		done_at TEXT,
		checked_at TEXT,
		checked_by TEXT NOT NULL DEFAULT '',
		idem TEXT,
		UNIQUE(author, idem));
	CREATE TABLE IF NOT EXISTS request_notes(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		request_id INTEGER NOT NULL REFERENCES requests(id),
		text TEXT NOT NULL,
		created_at TEXT NOT NULL);
	CREATE INDEX IF NOT EXISTS idx_request_notes_request ON request_notes(request_id);`)
	if err != nil {
		return err
	}
	if err = addColumns(db, []columnAdd{
		{"reports", "request_id", `INTEGER`},
		{"messages", "request_id", `INTEGER`},
		{"questions", "request_id", `INTEGER`},
	}); err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_reports_request ON reports(request_id) WHERE request_id IS NOT NULL;
	CREATE INDEX IF NOT EXISTS idx_messages_request ON messages(request_id) WHERE request_id IS NOT NULL;
	CREATE INDEX IF NOT EXISTS idx_questions_request ON questions(request_id) WHERE request_id IS NOT NULL;`)
	return err
}

// migrateRoleLifecycle adds roles.rotated_at (when an operator last replaced
// the role's token) and roles.retired_at (when the role was retired: revoked
// for good, its row and mail kept), both RFC 3339 text, null when never (#7).
func migrateRoleLifecycle(db *sql.DB) error {
	return addColumns(db, []columnAdd{
		{"roles", "rotated_at", `TEXT`},
		{"roles", "retired_at", `TEXT`},
	})
}

// migrateRuleProposals adds the rules ledger's running gear (#409,
// internal/rules), all additive: rules.reviewed_by, reviewed_at and
// review_note (who approved or rejected a proposed row, when, and why);
// rule_proposers (the roles an admin or a global boss let propose tips);
// rule_classes (a person's answer to a panel question since this table
// came in, unclassified until its asker marks it: a rule's number or once).
func migrateRuleProposals(db *sql.DB) error {
	for _, c := range []string{"reviewed_by", "reviewed_at", "review_note"} {
		has, err := HasColumn(db, "rules", c)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE rules ADD COLUMN ` + c + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS rule_proposers(role TEXT PRIMARY KEY, set_by TEXT NOT NULL, set_at TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS rule_classes(
		question_id TEXT PRIMARY KEY,
		asker TEXT NOT NULL,
		header TEXT NOT NULL DEFAULT '',
		answered_at TEXT NOT NULL,
		class TEXT NOT NULL DEFAULT '',
		rule_id INTEGER,
		classified_by TEXT NOT NULL DEFAULT '',
		classified_at TEXT NOT NULL DEFAULT '');
	CREATE INDEX IF NOT EXISTS idx_rule_classes_asker ON rule_classes(class, asker);`)
	return err
}

// migrateRuleHits adds rule_hits (#408 T2, internal/rules): per rule row,
// how many times sessions hit it and when last.
func migrateRuleHits(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS rule_hits(rule_id INTEGER PRIMARY KEY, hits INTEGER NOT NULL DEFAULT 0, last_at TEXT NOT NULL DEFAULT '')`)
	return err
}

// migrateRules adds the rules ledger (#406 ①, internal/rules): rules (a
// decision or tip, its group, scope and topic, its sources, state and the
// rows it replaced; match_* are the hit conditions ② reads), rule_topics
// (the topics a rule picks from), service_tags (tags a service's roles
// inherit) and role_tags (a role's additions, op '+', and removals, '-').
func migrateRules(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS rules(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		body TEXT NOT NULL,
		grp TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL,
		scope_values TEXT NOT NULL DEFAULT '',
		topic TEXT NOT NULL,
		sources TEXT NOT NULL DEFAULT '',
		source_text TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active',
		replaces TEXT NOT NULL DEFAULT '',
		replaced_by INTEGER,
		device TEXT NOT NULL DEFAULT '',
		match_tool TEXT NOT NULL DEFAULT '',
		match_command TEXT NOT NULL DEFAULT '',
		match_path TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL,
		created_at TEXT NOT NULL,
		voided_by TEXT NOT NULL DEFAULT '',
		voided_at TEXT NOT NULL DEFAULT '',
		idem TEXT,
		UNIQUE(author, idem));
	CREATE INDEX IF NOT EXISTS idx_rules_topic ON rules(grp, topic, status);
	CREATE TABLE IF NOT EXISTS rule_topics(name TEXT PRIMARY KEY, created_by TEXT NOT NULL, created_at TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS service_tags(service TEXT NOT NULL, tag TEXT NOT NULL, PRIMARY KEY(service, tag));
	CREATE TABLE IF NOT EXISTS role_tags(role TEXT NOT NULL, tag TEXT NOT NULL, op TEXT NOT NULL, PRIMARY KEY(role, tag));`)
	return err
}

// migrateHostGrants adds host_grants (#369): per role, the base roles, nodes
// and directory roots (JSON lists) it may host sessions of under itself with
// its own token; and hosted_sessions.dir_root, the grant root a role's record
// was added under (empty for the admin's).
func migrateHostGrants(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS host_grants(role TEXT PRIMARY KEY, bases TEXT NOT NULL, nodes TEXT NOT NULL, dir_roots TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	has, err := HasColumn(db, "hosted_sessions", "dir_root")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE hosted_sessions ADD COLUMN dir_root TEXT NOT NULL DEFAULT ''`)
	return err
}

// migrateHostAutoAlerts adds host_autos.alert (#336): the result (error,
// timeout or overrun) a scheduled run was last alerted for and that has not
// been resolved since; ” when none.
func migrateHostAutoAlerts(db *sql.DB) error {
	has, err := HasColumn(db, "host_autos", "alert")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE host_autos ADD COLUMN alert TEXT NOT NULL DEFAULT ''`)
	return err
}

// migrateHostAutoChecks adds host_autos.precheck and precheck_timeout_ms
// (#334): the command a scheduled run's node runs first (” none) and its
// limit in ms.
func migrateHostAutoChecks(db *sql.DB) error {
	adds := []struct{ column, def string }{
		{"precheck", `TEXT NOT NULL DEFAULT ''`},
		{"precheck_timeout_ms", `INTEGER NOT NULL DEFAULT 0`},
	}
	for _, c := range adds {
		has, err := HasColumn(db, "host_autos", c.column)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE host_autos ADD COLUMN ` + c.column + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateRoleCredentials adds role_credentials (#343): extra tokens of a
// role, kept as hashes like roles.token, each issued for one hosted record
// on one node. The role's own token is untouched. Times are Unix ms.
func migrateRoleCredentials(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS role_credentials(id TEXT PRIMARY KEY, role TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, record TEXT NOT NULL DEFAULT '', node TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, revoked_at INTEGER);
CREATE INDEX IF NOT EXISTS role_credentials_role ON role_credentials(role);`)
	return err
}

// migrateHostAutos adds host_autos and host_auto_runs (#333): scheduled runs
// of a hosted session (cron read in tz, the host's zone at creation;
// next_fire_at is the next time not yet handled, run_requested_at a manual
// run not yet handled; notify is a JSON list of roles) and their history,
// the newest 500 per run, dropped with it. Times are Unix ms.
func migrateHostAutos(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS host_autos(id INTEGER PRIMARY KEY AUTOINCREMENT, session TEXT NOT NULL, cron TEXT NOT NULL, tz TEXT NOT NULL, prompt TEXT NOT NULL, label TEXT NOT NULL DEFAULT '', resume INTEGER NOT NULL DEFAULT 0, notify TEXT NOT NULL DEFAULT '[]', state TEXT NOT NULL DEFAULT 'active', next_fire_at INTEGER, run_requested_at INTEGER, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS host_autos_session ON host_autos(session);
CREATE TABLE IF NOT EXISTS host_auto_runs(id INTEGER PRIMARY KEY AUTOINCREMENT, auto INTEGER NOT NULL, at INTEGER NOT NULL, result TEXT NOT NULL, manual INTEGER NOT NULL DEFAULT 0, exit_code INTEGER, duration_ms INTEGER, output TEXT NOT NULL DEFAULT '', mail_id INTEGER, launched INTEGER NOT NULL DEFAULT 0, agent_session_id TEXT NOT NULL DEFAULT '', detail TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS host_auto_runs_auto ON host_auto_runs(auto, id);`)
	return err
}

// migrateHostAutoStops adds the stop rules of scheduled runs (#335), all
// additive: host_autos.max_run_ms (how long a session a run launched may
// run, default 2h) and host_auto_runs.launched_at (when the run launched the
// session, the max-run clock's start) and ended_at (when the host stopped
// watching the session it launched; null while it watches). Times are Unix
// ms.
func migrateHostAutoStops(db *sql.DB) error {
	adds := []struct{ table, column, def string }{
		{"host_autos", "max_run_ms", `INTEGER NOT NULL DEFAULT 7200000`},
		{"host_auto_runs", "launched_at", `INTEGER`},
		{"host_auto_runs", "ended_at", `INTEGER`},
	}
	for _, c := range adds {
		has, err := HasColumn(db, c.table, c.column)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.column + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateQuestionRefusals adds question_refusals (#286): per role, the last
// can_ask refusal (the matched settings target, the ask_via role, the time
// in RFC 3339) and how many there were.
func migrateQuestionRefusals(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS question_refusals(role TEXT PRIMARY KEY, target TEXT NOT NULL, ask_via TEXT NOT NULL DEFAULT '', last_at TEXT NOT NULL, count INTEGER NOT NULL DEFAULT 0)`)
	return err
}

// migrateHostedEvents adds hosted_events (#252): each hosted session's
// starts, restarts, stops, crashes, exits and adoptions as its node reported
// them, the newest 200 per session, dropped with the session. A repeat of
// the same event (the node sends what it holds on every connection) is
// ignored by the unique key. Times are Unix ms.
func migrateHostedEvents(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS hosted_events(id INTEGER PRIMARY KEY AUTOINCREMENT, session TEXT NOT NULL, at INTEGER NOT NULL, kind TEXT NOT NULL, exit_code INTEGER, reason TEXT NOT NULL DEFAULT '', restarts INTEGER NOT NULL DEFAULT 0, actor TEXT NOT NULL DEFAULT '', UNIQUE(session, at, kind, restarts));
CREATE INDEX IF NOT EXISTS hosted_events_session ON hosted_events(session, at);`)
	return err
}

// migrateAdminActions adds admin_actions (#446): the admin's hosting actions
// (node invites and revokes, host grants, hosted session adds, changes and
// removals), appended only. target is a name or a path, never a value: no
// code, token, body or file content. method is the admin login mode ("boss"
// for a boss role's write); addr the client address. Times are Unix ms.
func migrateAdminActions(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS admin_actions(id INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, admin TEXT NOT NULL, method TEXT NOT NULL DEFAULT '', action TEXT NOT NULL, target TEXT NOT NULL DEFAULT '', addr TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS admin_actions_target ON admin_actions(target, at);`)
	if err != nil {
		return err
	}
	// detail (#448): what else names the action, a file save's path and
	// size; never a value.
	return addColumns(db, []columnAdd{{"admin_actions", "detail", `TEXT NOT NULL DEFAULT ''`}})
}

func HasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, e := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if e != nil {
		return false, e
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			return false, e
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Role tokens are stored as TokenHashPrefix + hex(sha256(token)), never as
// the token itself. Tokens are high-entropy (registration asks for at least
// 24 characters), so a fast hash is enough; comparisons stay constant time.
// Neither tokens nor their hashes leave the server: no API response or log
// line carries them.
const TokenHashPrefix = "sha256:"

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return TokenHashPrefix + hex.EncodeToString(sum[:])
}

// migrateRoleTokens upgrades the roles table in place: it adds revoked_at and
// hashes every token that lacks the hash prefix. Before the first hash it
// copies those plain rows to roles_plain_backup (in the same transaction), so
// a failure part way leaves the table as it was. Rows that already carry the
// prefix are left alone, so running it again changes nothing.
//
// roles_plain_backup holds plain tokens. dropPlainBackup removes it on the
// next start, after copying the database file aside.
func migrateRoleTokens(db *sql.DB) error {
	has, err := HasColumn(db, "roles", "revoked_at")
	if err != nil {
		return err
	}
	if !has {
		if _, err = db.Exec(`ALTER TABLE roles ADD COLUMN revoked_at TEXT`); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT role,token FROM roles WHERE substr(token,1,7)<>?`, TokenHashPrefix)
	if err != nil {
		return err
	}
	plain := map[string]string{}
	for rows.Next() {
		var role, token string
		if err = rows.Scan(&role, &token); err != nil {
			rows.Close()
			return err
		}
		plain[role] = token
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(plain) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS roles_plain_backup(role TEXT PRIMARY KEY, token TEXT NOT NULL, created_at TEXT NOT NULL, backed_up_at TEXT NOT NULL)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR REPLACE INTO roles_plain_backup(role,token,created_at,backed_up_at) SELECT role,token,created_at,? FROM roles WHERE substr(token,1,7)<>?`, now, TokenHashPrefix); err != nil {
		return err
	}
	for role, token := range plain {
		if _, err = tx.Exec(`UPDATE roles SET token=? WHERE role=? AND token=?`, TokenHash(token), role, token); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	log.Printf("role tokens: hashed %d plain rows; plain copies kept in roles_plain_backup", len(plain))
	return nil
}

// now is the clock for names of copies made before a drop; tests replace it.
var now = time.Now

// dropPlainBackup removes roles_plain_backup, the plain tokens kept by
// migrateRoleTokens. It first copies the whole database next to its file as
// <file>.pre-drop-plain-backup-<UTC time>, mode 0600, never overwriting a file
// already there. When the copy cannot be made it logs a line, keeps the table
// and lets the start go on; the next start tries again. A database without
// the table is left alone.
func dropPlainBackup(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='roles_plain_backup'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	var rowCount int
	if err := db.QueryRow(`SELECT count(*) FROM roles_plain_backup`).Scan(&rowCount); err != nil {
		return err
	}
	copyPath, err := copyBeforeDrop(db)
	if err != nil {
		log.Printf("role tokens: kept roles_plain_backup; copy before drop failed: %v", err)
		return nil
	}
	if _, err = db.Exec(`DROP TABLE roles_plain_backup`); err != nil {
		return err
	}
	log.Printf("role tokens: dropped roles_plain_backup (%d rows); copy before drop at %s", rowCount, copyPath)
	return nil
}

func copyBeforeDrop(db *sql.DB) (string, error) {
	var seq int
	var name, file string
	if err := db.QueryRow(`SELECT seq,name,file FROM pragma_database_list WHERE name='main'`).Scan(&seq, &name, &file); err != nil {
		return "", err
	}
	if file == "" {
		return "", errors.New("the database has no file")
	}
	path := file + ".pre-drop-plain-backup-" + now().UTC().Format("20060102T150405Z")
	// Create the file first, so it never exists with a wider mode and an
	// existing one is never overwritten; VACUUM INTO accepts an empty file.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if err = ownerfile.RestrictToUser(path); err != nil {
		f.Close()
		_ = os.Remove(path)
		return "", err
	}
	f.Close()
	if _, err = db.Exec(`VACUUM INTO ?`, path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return "", err
	}
	return path, nil
}

// LabelColumns are added to messages in place; see migrateLabels.
var LabelColumns = []struct{ Name, Def string }{
	{"addressed_to", `TEXT`},   // the label address, for mail sent to one
	{"held_id", `INTEGER`},     // the ID the message had while held
	{"held_at", `TEXT`},        // RFC 3339; when the message was held
	{"delivered_at", `TEXT`},   // RFC 3339; when held mail moved to a session
	{"held_notice_at", `TEXT`}, // RFC 3339; for telling the sender nobody connected (not set here)
	{"withdrawn_at", `TEXT`},   // RFC 3339; when the sender took the unread mail back (#354)
}

func migrateLabels(db *sql.DB) error {
	for _, c := range LabelColumns {
		has, err := HasColumn(db, "messages", c.Name)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE messages ADD COLUMN ` + c.Name + ` ` + c.Def); err != nil {
				return err
			}
		}
	}
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_held ON messages(held_at) WHERE status='held'`)
	return err
}

// migrateDirective adds messages.directive (1: a reply sent with --directive,
// which the progress view counts as a directive) and the index the answer
// lookup by mention reads (mail.AnswerCandidates).
func migrateDirective(db *sql.DB) error {
	has, err := HasColumn(db, "messages", "directive")
	if err != nil {
		return err
	}
	if !has {
		if _, err = db.Exec(`ALTER TABLE messages ADD COLUMN directive INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sender_recipient ON messages(sender,recipient,created_at)`)
	return err
}

// presenceColumns are added to slots in place; see migratePresence.
var presenceColumns = []struct{ name, def string }{
	{"state", `TEXT NOT NULL DEFAULT ''`},
	{"context_tokens", `INTEGER NOT NULL DEFAULT -1`}, // -1: never reported
	{"last_turn_at", `INTEGER NOT NULL DEFAULT 0`},    // Unix ms, 0: never reported
	{"reported_at", `INTEGER NOT NULL DEFAULT 0`},     // Unix ms of the last register/heartbeat/takeover
	// offline_noticed_at is the place to remember that an offline transition
	// has been handled (for example, a parent notified). Every report resets
	// it to 0. See offlineUnnoticed and markOfflineNoticed.
	{"offline_noticed_at", `INTEGER NOT NULL DEFAULT 0`},
	// subagents is the last reported number of running background sub-agents
	// (0 when a report leaves it out, and on release).
	{"subagents", `INTEGER NOT NULL DEFAULT 0`},
	// cli_version is the X-Switchboard-Version of the last register or
	// heartbeat (#308): "unknown" when the slot was claimed without one (a
	// heartbeat without it keeps the value, #319), '' when the slot has not
	// reported since.
	{"cli_version", `TEXT NOT NULL DEFAULT ''`},
	// cli_version_seen_at is when cli_version was last observed (Unix ms, 0:
	// never), cli_version_source where from ("cli": the session's own CLI
	// request, "heartbeat": the plugin's header) and cli_version_claim the
	// header of the last register or heartbeat (#500): an unchanged claim
	// does not replace a version the CLI itself sent.
	{"cli_version_seen_at", `INTEGER NOT NULL DEFAULT 0`},
	{"cli_version_source", `TEXT NOT NULL DEFAULT ''`},
	{"cli_version_claim", `TEXT NOT NULL DEFAULT ''`},
}

func migratePresence(db *sql.DB) error {
	for _, c := range presenceColumns {
		has, err := HasColumn(db, "slots", c.name)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE slots ADD COLUMN ` + c.name + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	return nil
}

func migratePresenceLog(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS presence_log(id INTEGER PRIMARY KEY AUTOINCREMENT,base TEXT NOT NULL,number INTEGER NOT NULL,role TEXT NOT NULL,state TEXT NOT NULL,started_at INTEGER NOT NULL,ended_at INTEGER);
CREATE INDEX IF NOT EXISTS idx_presence_log_slot ON presence_log(base,number,ended_at);
CREATE INDEX IF NOT EXISTS idx_presence_log_started ON presence_log(started_at);`)
	return err
}

// migrateMailboxViews adds the audit log of boss views of other mailboxes
// (additive): viewer, mailbox, message ID (0 for a list) and time.
func migrateMailboxViews(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS mailbox_views(id INTEGER PRIMARY KEY AUTOINCREMENT,viewer TEXT NOT NULL,mailbox TEXT NOT NULL,message_id INTEGER NOT NULL DEFAULT 0,viewed_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS idx_mailbox_views_at ON mailbox_views(viewed_at)`)
	return err
}

// migrateLeftItems adds the ledger of what an answer that ended a directive
// left undone (#470, additive). kind is item (one thing left), none (the
// answerer said nothing is left; one per directive) or unrecorded (an older
// CLI ended the directive without saying; one per directive). state is open,
// issue, passed or dropped for item and unrecorded rows and none for a none
// row. ended_by, ended_at and value (the issue URL, the directive passed to,
// or why it was dropped) are set once the row leaves open.
func migrateLeftItems(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS left_items(id INTEGER PRIMARY KEY AUTOINCREMENT,directive_id INTEGER NOT NULL,kind TEXT NOT NULL,text TEXT NOT NULL DEFAULT '',recorded_by TEXT NOT NULL,recorded_at TEXT NOT NULL,state TEXT NOT NULL,ended_by TEXT NOT NULL DEFAULT '',ended_at TEXT NOT NULL DEFAULT '',value TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS idx_left_items_state ON left_items(state);
CREATE INDEX IF NOT EXISTS idx_left_items_directive ON left_items(directive_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_left_items_once ON left_items(directive_id,kind) WHERE kind IN ('none','unrecorded')`)
	return err
}

// migrateBossWrites adds the audit log of a boss role's dashboard writes
// through its token (#399, additive): role, method, path, target, status and
// time. Request bodies are never kept.
func migrateBossWrites(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS boss_writes(id INTEGER PRIMARY KEY AUTOINCREMENT,role TEXT NOT NULL,method TEXT NOT NULL,path TEXT NOT NULL,target TEXT NOT NULL DEFAULT '',status INTEGER NOT NULL DEFAULT 0,written_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS idx_boss_writes_at ON boss_writes(written_at)`)
	return err
}

// migrateLedgerViews adds the audit log of reads of one job or schedule
// with its command or prompt by anyone but its owner (#404, additive):
// viewer (an admin's name, admin=1, or a role), kind (job or schedule),
// item ID, owner role and time. The command and prompt are never kept.
func migrateLedgerViews(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS ledger_views(id INTEGER PRIMARY KEY AUTOINCREMENT,viewer TEXT NOT NULL,admin INTEGER NOT NULL DEFAULT 0,kind TEXT NOT NULL,item_id INTEGER NOT NULL,owner TEXT NOT NULL,viewed_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS idx_ledger_views_at ON ledger_views(viewed_at)`)
	return err
}

// migrateSchedulePauses adds a schedule's pause (#404, additive): when, by
// whom (a role, or an admin's name with paused_by_admin=1) and why, on
// schedules; and schedule_pauses, every pause and resume: schedule, action
// (pause or resume), actor, reason, what a resume did with a once fire that
// passed (fire, drop or ”), and time.
func migrateSchedulePauses(db *sql.DB) error {
	adds := []struct{ column, def string }{
		{"paused_at", `TEXT`},
		{"paused_by", `TEXT NOT NULL DEFAULT ''`},
		{"paused_by_admin", `INTEGER NOT NULL DEFAULT 0`},
		{"pause_reason", `TEXT NOT NULL DEFAULT ''`},
	}
	for _, c := range adds {
		has, err := HasColumn(db, "schedules", c.column)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE schedules ADD COLUMN ` + c.column + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schedule_pauses(id INTEGER PRIMARY KEY AUTOINCREMENT,schedule_id INTEGER NOT NULL,action TEXT NOT NULL,actor TEXT NOT NULL,admin INTEGER NOT NULL DEFAULT 0,reason TEXT NOT NULL DEFAULT '',passed TEXT NOT NULL DEFAULT '',at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS idx_schedule_pauses_schedule ON schedule_pauses(schedule_id)`)
	return err
}

// migrateAlerts adds the alerts table (additive).
func migrateAlerts(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS alerts(kind TEXT NOT NULL,key TEXT NOT NULL,day TEXT NOT NULL,sent_at INTEGER NOT NULL,PRIMARY KEY(kind,key,day))`)
	return err
}

func migratePermissions(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS permissions(id TEXT PRIMARY KEY,requester TEXT NOT NULL,requester_base TEXT NOT NULL,tool TEXT NOT NULL,summary TEXT NOT NULL,declared_parent TEXT NOT NULL DEFAULT '',parent TEXT NOT NULL DEFAULT '',parent_base TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,expires_at INTEGER NOT NULL,decision TEXT NOT NULL DEFAULT '',note TEXT NOT NULL DEFAULT '',answered_by TEXT NOT NULL DEFAULT '',answered_at INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS idx_permissions_requester ON permissions(requester_base,created_at);
CREATE INDEX IF NOT EXISTS idx_permissions_parent ON permissions(parent_base,created_at);`)
	if err != nil {
		return err
	}
	// idem (#32): the Idempotency-Key of the request, '' when none was sent.
	has, err := HasColumn(db, "permissions", "idem")
	if err != nil {
		return err
	}
	if !has {
		if _, err = db.Exec(`ALTER TABLE permissions ADD COLUMN idem TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_permissions_idem ON permissions(requester,idem) WHERE idem<>''`)
	return err
}

func migrateQuestionSettings(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS question_settings(target TEXT PRIMARY KEY,can_ask INTEGER NOT NULL DEFAULT 0,watchers TEXT NOT NULL DEFAULT '[]',discord INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL)`); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO question_settings(target,can_ask,watchers,discord,updated_at) VALUES(?,0,'[]',0,?)`, DefaultQuestionTarget, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// migrateGroups adds the groups table (additive). revoked_at is set when the
// group is revoked; its roles are revoked at the same time.
func migrateGroups(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS groups(prefix TEXT PRIMARY KEY, boss_role TEXT NOT NULL, created_at TEXT NOT NULL, revoked_at TEXT)`)
	return err
}

func migrateAdminAuth(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS admin_users(name TEXT PRIMARY KEY, password_hash TEXT NOT NULL, totp_secret TEXT NOT NULL, totp_last_step INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, disabled_at TEXT);
CREATE TABLE IF NOT EXISTS admin_sessions(id_hash TEXT PRIMARY KEY, user TEXT NOT NULL, mode TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS idx_admin_sessions_user ON admin_sessions(user);
CREATE TABLE IF NOT EXISTS admin_perms(name TEXT PRIMARY KEY, perms TEXT NOT NULL, updated_at TEXT NOT NULL);`)
	return err
}

// migrateInvites adds the invites table (additive). code_hash is
// TokenHash(normalized code); the code itself is never stored.
func migrateInvites(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS invites(id INTEGER PRIMARY KEY AUTOINCREMENT, code_hash TEXT NOT NULL UNIQUE, prefix TEXT NOT NULL, role TEXT, expires_at TEXT NOT NULL, used_at TEXT, used_by TEXT, created_by TEXT NOT NULL, created_at TEXT NOT NULL, cancelled_at TEXT)`)
	return err
}

func migrateQuestions(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS questions(id TEXT PRIMARY KEY,asker TEXT NOT NULL,asker_base TEXT NOT NULL,questions TEXT NOT NULL,answers TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'pending',answered_by TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,answered_at INTEGER NOT NULL DEFAULT 0,withdrawn_at INTEGER NOT NULL DEFAULT 0,seq INTEGER NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS idx_questions_seq ON questions(seq);
CREATE INDEX IF NOT EXISTS idx_questions_asker ON questions(asker_base,created_at);
CREATE INDEX IF NOT EXISTS idx_questions_answered ON questions(status,answered_at);`)
	if err != nil {
		return err
	}
	// answerer: 'owner' when no boss role could answer at ask time.
	has, err := HasColumn(db, "questions", "answerer")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE questions ADD COLUMN answerer TEXT NOT NULL DEFAULT ''`)
	return err
}

// migrateQuestionRelay adds questions.relay_to (#394): a JSON array of the
// addresses that also get the answer's text, ” when none (additive).
func migrateQuestionRelay(db *sql.DB) error {
	has, err := HasColumn(db, "questions", "relay_to")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE questions ADD COLUMN relay_to TEXT NOT NULL DEFAULT ''`)
	return err
}

// askerSlotColumns record the live slot a question was asked from (empty when
// the asker held no slot): its answer follows that session (internal/asks).
var askerSlotColumns = []string{"asker_machine", "asker_directory", "asker_label"}

// migrateHandoffs adds the handoffs table (held mail waiting for the next
// matching session, internal/mail/handoff.go) and the asker slot columns of
// questions (additive).
func migrateHandoffs(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS handoffs(message_id INTEGER PRIMARY KEY, base TEXT NOT NULL, machine TEXT NOT NULL, directory TEXT NOT NULL, label TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_handoffs_base ON handoffs(base);`); err != nil {
		return err
	}
	for _, c := range askerSlotColumns {
		has, err := HasColumn(db, "questions", c)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE questions ADD COLUMN ` + c + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	// handoffs.strict (#364): system mail for one folder's session (a
	// scheduled run's) matches as strictly as mail a role sent. The held_id
	// index finds mail delivered under a new id (mail.CurrentID).
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_held_id ON messages(held_id) WHERE held_id IS NOT NULL`); err != nil {
		return err
	}
	has, err := HasColumn(db, "handoffs", "strict")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE handoffs ADD COLUMN strict INTEGER NOT NULL DEFAULT 0`)
	return err
}

// InTx runs f in one transaction on db: it commits when f returns nil and
// rolls back otherwise.
func InTx(ctx context.Context, db *sql.DB, f func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// migrateRoleAliases adds role_aliases: an old role name (left by a rename)
// and the role it now points to. Mail and tokens under the old name reach the
// new role.
func migrateRoleAliases(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS role_aliases(alias TEXT PRIMARY KEY, role TEXT NOT NULL, created_at TEXT NOT NULL)`)
	return err
}

// migrateHosting adds the terminal hosting tables (#173): hosted_sessions,
// the host's records of what should run on a node, nodes, the node
// registry, and node_invites, the one-time join codes (hashed, #193). All
// stay empty unless hosting is used. Times are Unix ms.
func migrateHosting(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS hosted_sessions(id TEXT PRIMARY KEY, node TEXT NOT NULL, base TEXT NOT NULL, label TEXT NOT NULL DEFAULT '', parent TEXT NOT NULL DEFAULT '', profile TEXT NOT NULL, directory TEXT NOT NULL, agent_session_id TEXT NOT NULL, launched INTEGER NOT NULL DEFAULT 0, args TEXT NOT NULL DEFAULT '[]', env TEXT NOT NULL DEFAULT '{}', restart TEXT NOT NULL DEFAULT 'always', desired TEXT NOT NULL DEFAULT 'running', cols INTEGER NOT NULL DEFAULT 120, rows INTEGER NOT NULL DEFAULT 40, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, UNIQUE(node, base, directory));
CREATE TABLE IF NOT EXISTS nodes(name TEXT PRIMARY KEY, token_hash TEXT NOT NULL, machine TEXT NOT NULL DEFAULT '', os TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, last_seen_at INTEGER, revoked_at INTEGER);
CREATE TABLE IF NOT EXISTS node_invites(code_hash TEXT PRIMARY KEY, name TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, used_at INTEGER);`)
	if err != nil {
		return err
	}
	// restart_gen and start_gen count Restart and Start requests (#188); a
	// node acts when one grows, so `host restart` needs no server channel.
	for _, c := range []string{"restart_gen", "start_gen"} {
		has, err := HasColumn(db, "hosted_sessions", c)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE hosted_sessions ADD COLUMN ` + c + ` INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateTerminalLogPeer adds terminal_log.mode (the socket's view or input)
// and terminal_log.ip (the client address) (#367).
func migrateTerminalLogPeer(db *sql.DB) error {
	for _, c := range []string{"mode", "ip"} {
		has, err := HasColumn(db, "terminal_log", c)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE terminal_log ADD COLUMN ` + c + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateTerminalLog adds terminal_log (#192): who attached to, detached
// from or typed into a hosted terminal and when, with the byte count of
// input per socket per minute. Input content is never stored. Times are
// Unix ms; rows older than 30 days are pruned by the sweep.
func migrateTerminalLog(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS terminal_log(id INTEGER PRIMARY KEY AUTOINCREMENT, session TEXT NOT NULL, admin_user TEXT NOT NULL, kind TEXT NOT NULL, at INTEGER NOT NULL, bytes INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS terminal_log_session ON terminal_log(session, at);
CREATE INDEX IF NOT EXISTS terminal_log_at ON terminal_log(at);`)
	return err
}

// migrateHostingLimit adds the session cap and subscription limit state
// (#190), all additive: hosted_sessions.class (what the session does),
// nodes.max_sessions (the cap the node reported, 0 before it did),
// nodes.limit_since and limit_reset (the usage limit its sessions' screens
// show, null when none) and hosting_limit, one row: the operator's limit
// switch and when the limit last cleared. Times are Unix ms.
// migrateHostingAdapters adds the agent adapter columns (#434), all
// additive: hosted_sessions.adapter (empty: the node's default),
// nodes.adapters (a JSON list the node reported, empty before it did) and
// nodes.default_adapter, and host_grants.adapters (a JSON list, empty: any).
func migrateHostingAdapters(db *sql.DB) error {
	return addColumns(db, []columnAdd{
		{"hosted_sessions", "adapter", `TEXT NOT NULL DEFAULT ''`},
		{"nodes", "adapters", `TEXT NOT NULL DEFAULT ''`},
		{"nodes", "default_adapter", `TEXT NOT NULL DEFAULT ''`},
		{"host_grants", "adapters", `TEXT NOT NULL DEFAULT ''`},
		// nodes.agent_usage: the usage of the node's agent accounts, JSON
		// of the last state frame's (#434).
		{"nodes", "agent_usage", `TEXT NOT NULL DEFAULT ''`},
	})
}

// migrateWorkGroups adds work groups (#435), all additive: work_groups (a
// name the operator chooses and its remaining-limit threshold, a
// percentage; null is none), hosted_sessions.work_group (empty: none) and
// host_grants.groups (a JSON list of the groups the role may choose);
// account_limits (per agent account: the operator's remaining value, null
// none, and when the account's limit last cleared) and
// hosted_sessions.agent_account (the account the node reported the session
// uses, empty: the adapter's default).
// hosted_sessions.class (#190) stays, unread: work groups replaced the
// session classes, and migrations only add.
func migrateWorkGroups(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS work_groups(name TEXT PRIMARY KEY, threshold INTEGER, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
	CREATE TABLE IF NOT EXISTS account_limits(node TEXT NOT NULL, adapter TEXT NOT NULL, account TEXT NOT NULL, manual INTEGER, set_at INTEGER NOT NULL DEFAULT 0, cleared_at INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(node,adapter,account))`); err != nil {
		return err
	}
	return addColumns(db, []columnAdd{
		{"hosted_sessions", "work_group", `TEXT NOT NULL DEFAULT ''`},
		{"host_grants", "groups", `TEXT NOT NULL DEFAULT ''`},
		{"hosted_sessions", "agent_account", `TEXT NOT NULL DEFAULT ''`},
	})
}

// columnAdd is one additive column.
type columnAdd struct{ table, column, def string }

// addColumns adds each column its table lacks.
func addColumns(db *sql.DB, adds []columnAdd) error {
	for _, c := range adds {
		has, err := HasColumn(db, c.table, c.column)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.column + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateHostingLimit(db *sql.DB) error {
	adds := []struct{ table, column, def string }{
		{"hosted_sessions", "class", `TEXT NOT NULL DEFAULT 'execute'`},
		{"nodes", "max_sessions", `INTEGER NOT NULL DEFAULT 0`},
		{"nodes", "limit_since", `INTEGER`},
		{"nodes", "limit_reset", `INTEGER`},
	}
	for _, c := range adds {
		has, err := HasColumn(db, c.table, c.column)
		if err != nil {
			return err
		}
		if !has {
			if _, err = db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.column + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS hosting_limit(id INTEGER PRIMARY KEY CHECK(id=1), operator INTEGER NOT NULL DEFAULT 0, set_at INTEGER NOT NULL DEFAULT 0, cleared_at INTEGER NOT NULL DEFAULT 0)`)
	return err
}

// migrateHostingPermissions adds hosted_sessions.skip_permissions (#220):
// 1 launches the agent with its permission-skip arguments. Records made
// before it get 0, so a running installation keeps its behaviour; new
// records are written with 1 unless asked otherwise.
func migrateHostingPermissions(db *sql.DB) error {
	has, err := HasColumn(db, "hosted_sessions", "skip_permissions")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE hosted_sessions ADD COLUMN skip_permissions INTEGER NOT NULL DEFAULT 0`)
	return err
}

// migrateRemind adds messages.no_reply (1: sent with --no-reply, never an
// open directive) and remind_state, whose one row records when directive
// reminders were first switched on (presence.SweepReminders). The row is
// written by the first sweep with SWITCHBOARD_REMIND=on, never here.
func migrateRemind(db *sql.DB) error {
	has, err := HasColumn(db, "messages", "no_reply")
	if err != nil {
		return err
	}
	if !has {
		if _, err = db.Exec(`ALTER TABLE messages ADD COLUMN no_reply INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS remind_state(id INTEGER PRIMARY KEY CHECK(id=1), enabled_at INTEGER NOT NULL)`)
	return err
}

// migrateAnsweredRule adds answered_rule, whose one row records the version
// of the answered rule (mail.AnsweredRuleVersion) the stored directives were
// last settled under. The row is written at start-up by
// presence.MigrateAnsweredRule, never here. answered_rule_log keeps each
// step it ran (versions, count, the marked message IDs) after the log line
// is gone (#274).
func migrateAnsweredRule(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS answered_rule(id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL, updated_at TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS answered_rule_log(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_version INTEGER NOT NULL,
		to_version INTEGER NOT NULL,
		marked INTEGER NOT NULL,
		message_ids TEXT NOT NULL,
		at TEXT NOT NULL)`)
	return err
}

// migrateReports adds reports (a boss role's report, #262) and report_mail,
// the threads a report links by their root message ID. A report's status is
// derived from those threads, never stored.
func migrateReports(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS reports(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		author TEXT NOT NULL,
		title TEXT NOT NULL,
		request TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL,
		created_at TEXT NOT NULL,
		idem TEXT,
		UNIQUE(author, idem));
	CREATE TABLE IF NOT EXISTS report_mail(
		report_id INTEGER NOT NULL REFERENCES reports(id),
		message_id INTEGER NOT NULL,
		PRIMARY KEY(report_id, message_id));
	CREATE INDEX IF NOT EXISTS idx_report_mail_message ON report_mail(message_id);`)
	return err
}

// migrateJobs adds jobs (#289): commands handed to a detached runner on the
// owner role's machine. state is handed, running or an end state (wire.Job*);
// times are RFC 3339; idem is unique per role; stop_requested is the one
// thing that travels down to the runner.
func migrateJobs(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS jobs(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		role TEXT NOT NULL,
		machine TEXT NOT NULL,
		kind TEXT NOT NULL DEFAULT 'command',
		label TEXT NOT NULL DEFAULT '',
		command TEXT NOT NULL,
		cwd TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'handed',
		exit_code INTEGER,
		error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		started_at TEXT,
		ended_at TEXT,
		expires_at TEXT NOT NULL,
		stop_requested_at TEXT,
		tail TEXT NOT NULL DEFAULT '',
		idem TEXT,
		UNIQUE(role, idem));
	CREATE INDEX IF NOT EXISTS idx_jobs_role ON jobs(role, id);
	CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs(state);`)
	if err != nil {
		return err
	}
	if err = migrateJobDuration(db); err != nil {
		return err
	}
	if err = addNoExtend(db, "jobs"); err != nil {
		return err
	}
	return migrateJobSeen(db)
}

// addNoExtend adds no_extend (#307) to a jobs or schedules table: 1 when the
// row was created with --no-extend, so the presence sweep never moves its
// expiry. Rows from before keep 0.
func addNoExtend(db *sql.DB, table string) error {
	has, err := HasColumn(db, table, "no_extend")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN no_extend INTEGER NOT NULL DEFAULT 0`)
	return err
}

// migrateJobDuration adds jobs.duration_s (#291): the expiry the job was
// created with, in seconds. Auto-extension moves expires_at to now plus it.
// Rows from before keep 0 and are never extended.
func migrateJobDuration(db *sql.DB) error {
	has, err := HasColumn(db, "jobs", "duration_s")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE jobs ADD COLUMN duration_s INTEGER NOT NULL DEFAULT 0`)
	return err
}

// migrateJobSeen adds jobs.seen_at (#290): the runner's last report. A live
// job whose runner stays silent past wire.JobSilentAfter becomes lost; rows
// from before count from created_at.
func migrateJobSeen(db *sql.DB) error {
	has, err := HasColumn(db, "jobs", "seen_at")
	if err != nil || has {
		return err
	}
	_, err = db.Exec(`ALTER TABLE jobs ADD COLUMN seen_at TEXT`)
	return err
}

// migrateSchedules adds schedules (#292): prompts the server delivers to the
// owner role as mail at fire times (wire.Schedule*). cron is the five-field
// expression read in tz (recorded at creation); next_fire_at is the next
// fire not yet handled, NULL once nothing is left to fire; missed and
// missed_last_at hold fires not yet delivered; last_mail_id is the newest
// fire mail, so at most one stays unread. Times are RFC 3339.
func migrateSchedules(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schedules(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		role TEXT NOT NULL,
		machine TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL,
		label TEXT NOT NULL DEFAULT '',
		prompt TEXT NOT NULL,
		cron TEXT NOT NULL DEFAULT '',
		tz TEXT NOT NULL DEFAULT 'UTC',
		state TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		duration_s INTEGER NOT NULL DEFAULT 0,
		next_fire_at TEXT,
		last_fire_at TEXT,
		missed INTEGER NOT NULL DEFAULT 0,
		missed_last_at TEXT,
		last_mail_id INTEGER,
		idem TEXT,
		UNIQUE(role, idem));
	CREATE INDEX IF NOT EXISTS idx_schedules_role ON schedules(role, id);
	CREATE INDEX IF NOT EXISTS idx_schedules_state ON schedules(state);`)
	if err != nil {
		return err
	}
	return addNoExtend(db, "schedules")
}

// migrateSystemSender renames the system sender's old stored name
// (agent-mail/system and its numbered forms) to switchboard/system, so mail
// sent under it keeps counting as system mail now that only one name is
// reserved (#427). A row that would collide on (sender, idem) is left.
func migrateSystemSender(db *sql.DB) error {
	_, err := db.Exec(`UPDATE OR IGNORE messages SET sender='switchboard/'||substr(sender,12) WHERE sender='agent-mail/system' OR sender GLOB 'agent-mail/system-[0-9]*'`)
	return err
}
