// Package cli is the client: every switchboard command, the
// credential profiles and the HTTP calls. It talks to the server over HTTP only.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/ownerfile"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Exit codes shared by every CLI command.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitUsage   = 2
	ExitTimeout = 3
	// ExitNodeRevoked: `node` stopped because the host refused its token
	// (revoked) (#203).
	ExitNodeRevoked = 4
)

type Config struct {
	Endpoint string `json:"endpoint"`
	Role     string `json:"role"`
	Token    string `json:"token"`
}

type option struct {
	name string // e.g. "--to"
	arg  string // value placeholder; empty for boolean switches
	help string
}

type command struct {
	name     string
	args     string // positional synopsis, e.g. "ID"
	nargs    int    // exact number of positional arguments (at most, with optArgs)
	optArgs  bool   // the positional arguments may be left out
	extra    bool   // arguments after -- are passed through (parsedArgs.extra), not positional
	synopsis string // extra usage text after the positional arguments
	summary  string
	example  string // one line shown right after the usage line (#562)
	options  []option
}

var (
	optJSON = option{"--json", "", "print the server's JSON response"}
	// optRequestID links a write to the owner's request (#501).
	optRequestID = option{"--request-id", "N", "the owner's request this belongs to (see `request list`)"}
	// optNoPlugin is also a global option, before the command (#477).
	optNoPlugin = option{"--no-plugin", "", "do not install the agent plugin this binary carries (also SWITCHBOARD_PLUGIN=off)"}
	optProfile  = option{"--profile", "NAME", "use the named credential profile (same as SWITCHBOARD_PROFILE)"}
	optBody     = option{"--body", "TEXT", "message body"}
	optBodyF    = option{"--body-file", "PATH", "read the message body from a file (stdin is used when neither is given)"}
	optSubject  = option{"--subject", "TEXT", "message subject"}
	optUrgent   = option{"--urgent", "", "mark the message urgent (default priority is normal)"}
	optNoReply  = option{"--no-reply", "", "the message needs no answer: never an open directive, never reminded"}
	// optLeft and optLeftNone go with an answer that ends a directive (#470):
	// reply --complete, complete and close (and send --complete).
	optLeft     = option{"--left", "ITEM", "an answer that ends a directive: one thing it left undone (repeatable); the directive's sender or a boss role over it ends it"}
	optLeftNone = option{"--left-none", "", "an answer that ends a directive: nothing is left undone"}
	// optVerdict goes with completing a [남긴 것] reminder (#565).
	optVerdict = option{"--verdict", "ITEM=VERDICT", "completing a [남긴 것] reminder: one item's verdict, done, keep (stays open, reminded again), drop:REASON or issue:URL (repeatable; every open item it lists needs one)"}
)

var commands = []command{
	{name: "register", synopsis: "--endpoint URL --role ROLE --token-file PATH --enrollment-token-file PATH",
		summary: "Register a new role or prove ownership of an existing role",
		options: []option{
			{"--endpoint", "URL", "service URL"},
			{"--role", "ROLE", "role to register"},
			{"--token-file", "PATH", "file holding the role credential"},
			{"--enrollment-token-file", "PATH", "file holding the enrollment credential"},
		}},
	{name: "join", args: "URL CODE", nargs: 2,
		summary: "Join a server with an invite code: makes the role token here and saves a profile",
		options: []option{
			{"--role", "ROLE", "role to take (a name under the invite's prefix; the invite's role when it names one)"},
			{"--profile", "NAME", "profile to write (default: from the server host); must not exist yet"},
			optNoPlugin,
		}},
	{name: "plugin", args: "ACTION", nargs: 1, synopsis: "(install | status) [--json]",
		summary: "Install the agent plugin this binary carries (its version is this CLI's) into the agent's user settings and turn off an older copy installed another way, or show what is installed; join and the first run of a new version install it too",
		options: []option{{"--json", "", "status: print the state as JSON"}}},
	{name: "invite", synopsis: "--prefix PREFIX [--role ROLE] [--ttl 72h]",
		summary: "Create a one-time invite code for a collaborator group (global boss roles only)",
		options: []option{
			{"--prefix", "PREFIX", "group prefix, e.g. partner-a; the group is created when missing"},
			{"--role", "ROLE", "pin the invite to one role under the prefix"},
			{"--ttl", "DURATION", "how long the code works, e.g. 72h or 7d (default 72h, at most 30d)"},
			optJSON, optProfile,
		}},
	{name: "config", args: "set", nargs: 1, synopsis: "--endpoint URL --role ROLE --token-file PATH",
		summary: "Save endpoint and role credential for this user profile",
		options: []option{
			{"--endpoint", "URL", "service URL"},
			{"--role", "ROLE", "role name"},
			{"--token-file", "PATH", "file holding the role credential"},
			optProfile,
		}},
	{name: "send", synopsis: "--to ROLE --subject TEXT [--complete ID[,ID...]]... (--body TEXT | --body-file PATH | stdin)",
		summary: "Send a message to a registered role",
		options: []option{
			{"--to", "ROLE", "recipient role, or label:BASE/LABEL for the session of BASE carrying LABEL"},
			optSubject, optBody, optBodyF,
			{"--task", "ID", "task identifier to attach"},
			optUrgent, optNoReply,
			{"--complete", "ID[,ID...]", "mail you received to mark complete with this send (repeatable; one transaction, refused unless every ID is yours)"},
			optLeft, optLeftNone, optVerdict,
			{"--idempotency-key", "KEY", "retry-safe key; resending with the same key returns the first message and marks nothing twice"},
			optRequestID, optJSON, optProfile,
		}},
	{name: "inbox", summary: "List messages for the active role (newest 100; --limit and --before page)",
		options: []option{
			{"--q", "TEXT", "only messages whose subject, body or task contain TEXT"},
			{"--status", "STATUS", "only messages in STATUS: pending, read, acknowledged or complete (completed also accepted)"},
			{"--limit", "N", "at most N messages (default 100, at most 500), newest first"},
			{"--before", "ID", "only messages with an ID below ID: the next page after the oldest one shown"},
			{"--of", "ROLE", "boss roles only: list another role's or slot's inbox read-only (states unchanged; the view is recorded)"},
			optJSON, optProfile,
		}},
	{name: "outbox", summary: "List messages sent by the active role (newest 100; --limit and --before page)",
		options: []option{
			{"--q", "TEXT", "only messages whose subject, body or task contain TEXT"},
			{"--status", "STATUS", "only messages whose recipient state is STATUS"},
			{"--limit", "N", "at most N messages (default 100, at most 500), newest first"},
			{"--before", "ID", "only messages with an ID below ID: the next page after the oldest one shown"},
			optJSON, optProfile,
		}},
	{name: "wait", summary: "Wait until the inbox has pending messages, print them and exit (does not mark them read)",
		options: []option{
			{"--timeout", "SECONDS", "give up after SECONDS and exit with code 3 (default 1800, 0 = no limit)"},
			{"--interval", "SECONDS", "seconds between checks (default 15)"},
			optJSON, optProfile,
		}},
	{name: "events", summary: "Long-poll once for new pending mail, print it and exit (does not mark it read)",
		options: []option{
			{"--after", "ID", "only messages with an ID greater than this cursor (default: every pending message)"},
			{"--timeout", "SECONDS", "wait up to SECONDS for new mail, capped by the server (default 25, 0 = answer at once); exit 3 when none arrives"},
			{"--json", "", "print the server's JSON response ({\"events\":[...],\"cursor\":N})"},
			optProfile,
		}},
	{name: "watch", args: "[start | list]", nargs: 1, optArgs: true, extra: true, synopsis: "[-- CMD ARGS...]",
		summary: "Watch inbox without consuming messages; watch start runs a command on this machine whose output lines are events (lines within a batch window make one urgent mail to this role; list, stop and wait it with job); watch list shows the server's API watches (#528) that mail this role (all of them for a global boss): name, receiving role, period and last result (registered on the dashboard only)",
		options: []option{
			{"--after", "ID", "only report messages with an ID greater than this cursor"},
			{"--attach", "", "start: stream the lines to stdout instead of mail while attached (killing it leaves the watch running and mail takes over)"},
			{"--expires", "DURATION", "start: how long it may run, e.g. 90m, 24h, 7d (default 7d; moves forward while a session of this role is attached)"},
			{"--batch", "DURATION", "start: batch window, lines within it make one mail (default 30s)"},
			{"--cwd", "DIR", "start: working directory (default the current one)"},
			{"--label", "TEXT", "start: label (default 명령 #ID)"},
			{"--no-extend", "", "start: the expiry is fixed; it does not move forward while a session is attached"},
			{"--idem", "KEY", "start: retry-safe key; starting again with the same key returns the first watch"},
			optJSON, optProfile,
		}},
	{name: "read", args: "ID", nargs: 1, summary: "Read a message (marks it read; a boss role may open another role's mail, which leaves its state unchanged and is recorded)", options: []option{optJSON, optProfile}},
	{name: "reply", args: "ID", nargs: 1, synopsis: "[--complete] (--body TEXT | --body-file PATH | stdin)", summary: "Reply to a message; reply --complete also marks it complete",
		options: []option{
			{"--complete", "", "the reply is the final word on ID: also mark ID complete (one transaction). Without it a directive stays open"},
			{"--done", "", "older name of --complete"},
			optSubject, optBody, optBodyF,
			{"--to", "ROLE", "send the reply to ROLE instead of the original sender"},
			optUrgent,
			{"--directive", "", "the reply carries new work: count it as a directive in progress"},
			optNoReply,
			optLeft, optLeftNone, optVerdict,
			{"--idempotency-key", "KEY", "retry-safe key; resending with the same key returns the first reply and marks nothing twice"},
			optRequestID,
			optJSON, optProfile,
		}},
	{name: "ack", args: "ID", nargs: 1, summary: "Acknowledge a message", options: []option{optJSON, optProfile}},
	{name: "complete", args: "ID", nargs: 1, summary: "Mark a message complete; completing a directive sent to you ends it, so say what is left (--left or --left-none)", options: []option{optLeft, optLeftNone, optVerdict, optJSON, optProfile}},
	{name: "close", args: "ID", nargs: 1, summary: "Close a directive you sent: it needs no answer (no longer open, never reminded); given an answer's ID, close the directive it answers and complete the answer. Closing a directive that has not ended ends it: say what is left (--left or --left-none)", options: []option{optLeft, optLeftNone, optJSON, optProfile}},
	{name: "left", args: "add N TEXT | issue ID URL | pass ID | done ID | drop ID | attach ID", nargs: 3, optArgs: true,
		summary: "What an answer that ended a directive left undone (진행 대기 남긴 것): add TEXT to directive N you ended; the directive's sender, the role that wrote the item on a report it got (new mail, not a reply) from a role it had directed first on the same task or request, or a boss role over the directive (a global boss for any; a group boss inside its groups), raises item ID as an issue (URL), passes it on in another directive the same sender sent (--directive M), marks it done, or drops it (--reason). The directive's sender or a boss role attaches item ID to an owner's request (--request-id; an item follows its directive's request until then). An item left open gets the sender a reminder each reminder time (설정 · 다시 알림)",
		options: []option{
			{"--directive", "M", "pass: the directive that takes the item over, sent by the item's directive's sender (you, unless a boss), or by you when you wrote the item on a report"},
			{"--reason", "TEXT", "drop: why the item is dropped, one line"},
			{"--request-id", "N", "attach: the owner's request the item belongs to (see `request list`)"},
			optJSON, optProfile,
		}},
	{name: "withdraw", args: "ID", nargs: 1, summary: "Take back mail you sent that its recipient has not read (409 once read: close it or send new mail)", options: []option{optJSON, optProfile}},
	{name: "slots", summary: "List the leased numbered slots under the active role's base",
		options: []option{optJSON, optProfile}},
	{name: "slot", args: "ACTION", nargs: 1, synopsis: "(ACTION: register | heartbeat | takeover | release)",
		summary: "Claim, renew, take over (-1) or release a numbered slot for this machine and directory",
		options: []option{
			{"--machine", "NAME", "machine identity (default: this host name)"},
			{"--directory", "PATH", "directory identity (default: the current directory)"},
			{"--session-id", "ID", "session id to record with the slot"},
			{"--label", "TEXT", "label to record with the slot"},
			{"--parent", "ROLE", "parent role to record with the slot"},
			{"--state", "STATE", "presence state to report: idle, busy, compacting, error:<kind> or blocked:<kind> (kind: 1-64 of A-Z a-z 0-9 . _ -) (register, heartbeat, takeover)"},
			{"--context-tokens", "N", "context size in tokens to report (register, heartbeat, takeover)"},
			{"--last-turn-at", "TIME", "time the last turn ended, RFC 3339 (register, heartbeat, takeover)"},
			{"--subagents", "N", "running background sub-agents to report; leaving it out reports 0 (register, heartbeat, takeover)"},
			optJSON, optProfile,
		}},
	{name: "status", args: "[ROLE]", nargs: 1, optArgs: true,
		summary: "Show session presence: every session, a base and its numbers, one numbered role, or a prefix ending in *",
		options: []option{optJSON, optProfile}},
	{name: "metrics", args: "[hourly]", nargs: 1, optArgs: true,
		summary: "Show mail-flow metrics: first-reply time, pending mail, back-and-forth threads, error/blocked session time; hourly: mail, reports and questions per hour over 24 hours and per role over the last hour (boss roles only)",
		options: []option{
			{"--days", "N", "window in days ending now, 1-90 (default 7; not with hourly)"},
			optJSON, optProfile,
		}},
	{name: "progress", args: "[SERVICE]", nargs: 1, optArgs: true,
		summary: "Show per-service progress: last report, live sessions, and open directives (! over the threshold) (boss roles only)",
		options: []option{{"--json", "", "print the server's whole JSON response unchanged (SERVICE is ignored)"}, optProfile}},
	{name: "report", args: "[list | show ID]", nargs: 2, optArgs: true,
		synopsis: "| --title TEXT [--request TEXT] [--mail ID[,ID...]]... (--body TEXT | --body-file PATH | stdin)",
		summary:  "Post a report to the dashboard, linking the directive threads it covers; prints the report ID. `report list [--open]` lists the reports (with --open, each open one's open directive threads) and `report show R<ID>` one report's threads (boss roles only)",
		options: []option{
			{"--open", "", "list: only open reports, each with its open directive threads"},
			{"--title", "TEXT", "report title (one line)"},
			{"--request", "TEXT", "what was asked (one line)"},
			{"--mail", "ID[,ID...]", "link mail by ID (repeatable); a reply links its thread's directive; mail you received is also marked complete; #N in the body is never a link"},
			{"--body", "TEXT", "report body (Markdown)"},
			{"--body-file", "PATH", "read the report body from a file (stdin is used when neither is given)"},
			{"--idem", "KEY", "retry-safe key; posting again with the same key returns the first report"},
			optRequestID,
			{"--json", "", "print the stored report as JSON"},
			optProfile,
		}},
	{name: "search", args: "PATTERN", nargs: 1,
		summary: "Find text in mail (subject, body), reports (title, body), panel questions (question, options, chosen answer), left items and the owner's requests (title, text, notes), newest first, 50 at a time; each result is a line naming the record and the command that opens it, then the matched line. PATTERN is a substring, or with --regex a Go regular expression (RE2 syntax: https://github.com/google/re2/wiki/Syntax; no backreferences or lookaround); both ignore case unless --case. A role in a group sees its own group only",
		options: []option{
			{"--regex", "", "PATTERN is a regular expression (RE2)"},
			{"--case", "", "match case (the default ignores it)"},
			{"--kind", "mail|report|question|left|request", "only this kind (repeatable)"},
			{"--role", "ROLE", "a sender or recipient, report author, asker or left item's recorder (a slot or label counts for its base)"},
			{"--thread", "N", "mail of thread N, the reports linking it and its left items"},
			{"--since", "DATE|TIME", "on or after this UTC date (YYYY-MM-DD) or RFC 3339 time"},
			{"--until", "DATE|TIME", "up to this UTC date (inclusive) or RFC 3339 time"},
			{"--limit", "N", "results per page (default 50, at most 200)"},
			{"--after", "CURSOR", "the next page: the cursor the previous page printed"},
			{"--json", "", "print the server's JSON response"},
			optProfile,
		}},
	{name: "decisions",
		summary: "Show the decision ledger (answered questions), newest first; a boss role sees all, other roles their own base",
		options: []option{
			{"--from", "DATE|TIME", "answered on or after this UTC date (YYYY-MM-DD) or RFC 3339 time"},
			{"--to", "DATE|TIME", "answered up to this UTC date (inclusive) or RFC 3339 time"},
			{"--role", "ROLE", "asker: an exact role or base, or PREFIX/*"},
			{"--q", "TEXT", "case-insensitive text in a header, question or chosen label"},
			{"--ndjson", "", "stream the server's ndjson export unchanged"},
			optJSON, optProfile,
		}},
	{name: "rules", args: "[list | show N | add TEXT | edit N [TEXT] | void N | approve N | reject N | proposer [ROLE on|off] | topics | tag list | tag set SERVICE TAG[,TAG] | tag role ROLE +TAG|-TAG|=TAG | mine | hit N[,N] | classify QUESTION | unclassified]", nargs: 4, optArgs: true,
		summary: "The rules ledger: the decisions and tips in force by service and topic. An admin or a global boss reads every row with its scope, sources, history and the answered questions it came from, adds (a row on the same scope and topic as one in force needs --replace N[,N] or --alongside), voids and sets tags; any other role lists and shows only its own group's rows in force that reach it (all, its service, its tags). A role's tags are its service's (tag set) plus +TAG, minus -TAG (tag role; put -- before a -TAG, =TAG goes back to the service's). edit N writes a new row replacing N with its fields, changed by TEXT and the options. mine lists the rows a session hook of this role matches tool calls against; hit counts their hits. A role an admin or a global boss turns on (proposer ROLE on) adds tips: on its own service alone they are in force at once, elsewhere they wait for approve N (through the replace flow) or reject N. A person's answer to your panel question stays unclassified (unclassified lists them) until classify QUESTION --rule N or --once, or a row added with it as a --source",
		options: []option{
			{"--service", "SERVICE", "list: the rows that reach the roles of SERVICE (the part of a role before /)"},
			{"--topic", "TOPIC", "list: only TOPIC; add: the row's topic, one of `rules topics`"},
			{"--kind", "decision|tip|all", "list: this kind (default decision; all for both); add: the row's kind"},
			{"--all", "", "list: both kinds, also replaced, void, proposed and rejected rows (admin or global boss)"},
			{"--proposed", "", "list: the proposed rows waiting for approval (admin or global boss)"},
			{"--scope", "all|service:NAME[,NAME]|tag:NAME[,NAME]", "add: where the row applies (all is the row's group)"},
			{"--group", "PREFIX", "add: the group of an all or tag row (default: roles in no group; a service row takes its services' group)"},
			{"--source", "ID[,ID]|TEXT", "add, edit: answered question IDs (q_…) or a text the row comes from (repeatable)"},
			{"--new-topic", "", "add: create --topic when it is not a topic yet"},
			{"--replace", "N[,N]", "add, approve: replace these rows in force"},
			{"--alongside", "", "add, approve: stand alongside the rows in force on the same scope and topic (with --replace, only those are replaced)"},
			{"--reason", "TEXT", "reject: why the proposal is turned down"},
			{"--rule", "N", "classify: the answer became rule N"},
			{"--once", "", "classify: the answer was for this job only"},
			{"--role", "ROLE", "unclassified: only ROLE's answers (admin or global boss)"},
			{"--device", "NAME", "add, edit: the mechanism that enforces the row (it is then never put into sessions)"},
			{"--when-tool", "TOOL", "add, edit: hit on this tool name (exact, or * patterns such as mcp__*); empty clears it"},
			{"--when-command", "RE", "add, edit: hit when the command text matches this regular expression; empty clears it"},
			{"--when-path", "GLOB", "add, edit: hit when the file path matches this glob (*, ** and ?); empty clears it"},
			{"--idem", "KEY", "add, edit: retry-safe key; adding again with the same key returns the first row"},
			{"--what", "TEXT", "hit: what the session was doing, kept on the hit line (one line, secrets hidden, up to 120 runes)"},
			optJSON, optProfile,
		}},
	{name: "mail", args: "ACTION [ID]", nargs: 2, optArgs: true, synopsis: "(ACTION: list | show ID | close ID)",
		summary: "Read every role's mail, the dashboard's mail tab (boss roles only; a group boss sees its groups): list newest first, show one message, leaving its state and recording the view, or close a directive as the dashboard does (recorded)",
		options: []option{
			{"--open", "", "list: only mail not complete"},
			{"--from", "ROLE", "list: only mail from ROLE"},
			{"--to", "ROLE", "list: only mail to ROLE"},
			{"--since", "DURATION", "list: only mail sent within DURATION, e.g. 90m, 24h, 7d"},
			{"--limit", "N", "list: at most N messages, 1-500 (default 100)"},
			optJSON, optProfile,
		}},
	{name: "jobs", args: "[show ID | stop ID | rm ID | rm-schedule ID]", nargs: 2, optArgs: true,
		summary: "List the server's job ledger and schedules of every role in view, the dashboard's jobs tab (boss roles only; commands and prompts left out); show one job with its command (the read is recorded; `schedule show ID` for a schedule's prompt), stop a job, remove an ended job's record or remove a schedule (rm and rm-schedule need --yes). `job list` is the same ledger for your own jobs",
		options: []option{
			{"--state", "STATE", "only jobs and schedules in STATE (live: not ended)"},
			optYes, optJSON, optProfile,
		}},
	{name: "hooks", args: "[list | add | edit NAME | rm NAME | rotate NAME]", nargs: 2, optArgs: true,
		summary: "List the incoming webhooks: name, receiving role, mode (mail or forward) and when the last notice came. A global boss role adds, changes, removes (needs --yes) them or makes a hook's address again (needs --yes), as the dashboard's 설정 › 웹훅 does: the address is written to a new owner-only --address-file only, the original destination read from a file or stdin, neither ever printed",
		options: []option{
			{"--name", "NAME", "add: the webhook's name"},
			{"--to", "ROLE", "add, edit: the role its notices are mailed to"},
			{"--address-file", "PATH", "add, rotate: a new file (it must not exist) the address is written to, owner-only, before the server is asked"},
			{"--kind", "mail|forward", "add, edit: mail only (the default), or forward the body on to the original destination"},
			{"--urgent", "", "add, edit: its alert mail is urgent"},
			{"--no-urgent", "", "edit: its alert mail is not urgent"},
			{"--window", "DURATION", "add, edit: notices of one subject within this collapse into one mail, whole minutes (default 10m)"},
			{"--destination-file", "PATH", "add, edit: read the original destination from a file"},
			{"--destination-stdin", "", "add, edit: read the original destination from stdin"},
			{"--clear-destination", "", "edit: remove the original destination"},
			{"--rename", "NEW", "edit: give the webhook a new name"},
			optYes, optJSON, optProfile,
		}},
	{name: "roles", args: "[revoke ROLE | restore ROLE | rotate ROLE | retire ROLE]", nargs: 2, optArgs: true,
		summary: "List roles (group, revoked, retired), leased numbered slots and groups, or revoke (needs --yes) or restore a role, rotate its token (needs --yes and --token-file) or retire it for good, keeping its mail (needs --yes) (boss roles only; a group boss sees and changes its groups)",
		options: []option{
			{"--token-file", "PATH", "rotate: the new role credential; an empty or missing file gets a new random one, written there owner-only before it is sent (never printed)"},
			optYes, optJSON, optProfile,
		}},
	{name: "settings", args: "[set TARGET | rm TARGET]", nargs: 2, optArgs: true,
		summary: "Show the dashboard's settings: question settings, recent refused questions and, for a global boss, the server summary and which secret settings are set (names only, never values); set or remove (needs --yes) a question setting of a role, PREFIX/* or * (boss roles only; a group boss its groups, * is a global boss's)",
		options: []option{
			{"--can-ask", "on|off", "set: the target may ask questions (left out: unchanged)"},
			{"--watchers", "ROLE[,ROLE]", "set: roles told about its questions; - for none (left out: unchanged)"},
			{"--discord", "on|off", "set: send its questions to Discord (left out: unchanged)"},
			optYes, optJSON, optProfile,
		}},
	{name: "permission", args: "ACTION [ID]", nargs: 2, optArgs: true, synopsis: "(ACTION: request | show ID | answer ID | list)",
		summary: "Ask for, show, answer or list tool permission requests (only a boss role answers; the requester's parent is notified)",
		options: []option{
			{"--tool", "NAME", "request: the tool asked for"},
			{"--summary", "TEXT", "request: what the tool will do (up to 500 bytes)"},
			{"--decision", "allow|deny", "answer: the decision"},
			{"--note", "TEXT", "answer: optional note for the requester"},
			{"--idempotency-key", "KEY", "request: retry-safe key; resending with the same key returns the first request and mails nobody twice"},
			{"--pending", "", "list: only requests still waiting for an answer"},
			optJSON, optProfile,
		}},
	{name: "question", args: "ACTION [ID]", nargs: 2, optArgs: true, synopsis: "(ACTION: ask | show ID | list | answer ID | withdraw ID)",
		summary: "Ask, show, list, answer or withdraw multiple-choice questions (only a boss role of another base answers; first answer wins)",
		options: []option{
			{"--file", "PATH", "ask: JSON with the questions ({\"questions\":[...]} or the bare list); answer: JSON with the answers ({\"answers\":[...]} or the bare list)"},
			{"--status", "STATUS", "list: only pending, answered or withdrawn questions"},
			{"--request-id", "N", "ask: the owner's request the questions belong to (see `request list`)"},
			optJSON, optProfile,
		}},
	{name: "request", args: "ACTION [ID]", nargs: 2, optArgs: true, synopsis: "(ACTION: add | note ID | wait ID | done ID | list | show ID)",
		summary: "The owner's requests (#501, 「지시」 on the dashboard; boss roles only): add registers one with the owner's words as given and a short title and prints its number; note ID adds the owner's later words; wait ID marks it waiting on the owner (대표 확인 대기: no reminder mail, --off clears it); done ID marks it done with a one-line result; list shows the open ones (not checked on the dashboard); show ID one request with its notes and the reports, panel questions and mail linked by --request-id. The dashboard checks a done request; the CLI cannot",
		options: []option{
			{"--title", "TEXT", "add: a short name (one line)"},
			{"--text", "TEXT", "add, note: the owner's words as given"},
			{"--text-file", "PATH", "add, note: read the words from a file (stdin is used when neither is given)"},
			{"--result", "TEXT", "done: the result (one line)"},
			{"--status", "STATUS", "list: only open (the default), waiting, done, progress or checked"},
			{"--all", "", "list: every request, checked ones too"},
			{"--off", "", "wait: clear the 대표 확인 대기 mark"},
			{"--idem", "KEY", "add: retry-safe key; adding again with the same key returns the first request"},
			optJSON, optProfile,
		}},
	{name: "host", args: "ACTION [ID]", nargs: 3, optArgs: true, extra: true,
		example: "Start a session of a new role: PROG host add --base team/worker --create-role --node NODE --profile NAME --dir PATH", synopsis: "(ACTION: add | list | nodes | set ID | start ID | stop ID | restart ID | new-conversation ID | remove ID | events ID | log ID | revoke-node NAME | limit [on|off|NODE/ADAPTER[/ACCOUNT] --remaining N|none] | grant ROLE | grant list | revoke-grant ROLE | group add NAME | group set NAME | group rm NAME | group list | auto add ID | auto list | auto show AUTO-ID | auto edit AUTO-ID | auto run AUTO-ID | auto pause AUTO-ID | auto resume AUTO-ID | auto rm AUTO-ID) [-- AGENT ARGS...]",
		summary: "Manage hosted sessions in this instance's database (DB_PATH, not the container default): add a record, list them, change one's permission mode, set one to run or stop, restart one with resume (clears a crash loop), start one in a new conversation, remove one, show or set the subscription limit; grant: let a role add, list, start, stop and remove sessions under itself with its own token (base roles, nodes and directory roots; without DB_PATH, host runs as the profile's role within its grant, and list and auto list of a boss role show every record in view); nodes: the node list for a boss role, through the server; group: work groups, each with a threshold (a session of the group takes no new work while its agent account has less than that percentage left; a boss role's token without DB_PATH works through the server); auto: scheduled runs of a session (at each cron time in this machine's zone the host launches it when stopped and mails the prompt to its base role); a boss role's token without DB_PATH makes the dashboard's terminal actions on the sessions in view (#399, recorded): set (also --label, --parent, --group, --restart, -- ARGS), start, stop, restart, new-conversation, remove, events, log, limit, revoke-node, auto show, edit, run, pause, resume and rm",
		options: []option{
			{"--node", "NAME", "add: node the session runs on"},
			{"--base", "ROLE", "add: base role (must exist, or --create-role; a boss role needs --another-boss)"},
			{"--create-role", "", "add: register the base role when it does not exist yet (a boss role's token, within its groups, or DB_PATH); without it a missing role is refused"},
			{"--another-boss", "", "add: let the base be a boss role, for one more session of it (without it such an add is refused)"},
			{"--profile", "NAME", "add: credential profile on the node (SWITCHBOARD_PROFILE)"},
			{"--dir", "PATH", "add: absolute working directory on the node"},
			{"--label", "TEXT", "add: label (SWITCHBOARD_LABEL); auto add, auto edit: the mail subject (default \"scheduled run ID\")"},
			{"--parent", "ROLE", "add: parent role (SWITCHBOARD_PARENT)"},
			{"--restart", "POLICY", "add: always (default), on-failure or never"},
			{"--group", "NAME", "add: work group (default: none, which always takes work); grant: work groups the role may choose (NAME[,NAME]; default: none)"},
			{"--threshold", "PERCENT|none", "group add, group set: remaining limit (0 to 100) below which the group's sessions take no new work (default: none)"},
			{"--remaining", "PERCENT|none", "limit NODE/ADAPTER[/ACCOUNT]: the operator's remaining limit (0 to 100) of that agent account, over what the agent reports; none clears it"},
			{"--adapter", "NAME", "add: agent adapter the session launches with (default: the node's); grant: adapters the role may choose (NAME[,NAME]; default: any)"},
			{"--dir-root", "PATH[,PATH]", "grant: directory roots the role's sessions must be under (with --base ROLE[,ROLE] and --node NAME[,NAME])"},
			{"--ask-permissions", "", "add: launch the agent in its normal permission mode (default: with its permission-skip arguments)"},
			{"--skip-permissions", "on|off", "set: launch with (on) or without (off) the agent's permission-skip arguments, from the next launch or restart"},
			{"--cron", "EXPR", "auto add, auto edit: five-field cron expression, read in this machine's time zone (edit: in the zone the run was added in)"},
			{"--prompt", "TEXT", "auto add, auto edit: the mail the session gets at each run"},
			{"--resume", "", "auto add: continue the session's last conversation when the run launches it (default: a new conversation each run)"},
			{"--notify", "ROLE[,ROLE]", "auto add: roles told about runs that need attention"},
			{"--max-run", "DURATION", "auto add: stop a session the run launched after this long (default 2h; it also stops when the run's mail is completed)"},
			{"--check", "CMD", "auto add: shell command the node runs in the session's directory before each run: exit 0 runs (its output follows the prompt), 1 skips, else error (the node needs --allow-checks)"},
			{"--check-timeout", "DURATION", "auto add: limit of --check, then it is killed with everything it started (default 60s, at most 10m)"},
			{"--runs", "N", "auto show: newest N runs (default 20, at most 500)"},
			{"--yes", "", "with a boss role's token: confirm remove, new-conversation, revoke-node and auto rm (without it the command says what it would do and exits 2)"},
			optJSON,
		}},
	{name: "serve",
		summary: "Run the mail server, as with no arguments; --check only checks its start-up settings and exits (1 when the server would refuse to start)",
		options: []option{
			{"--check", "", "run the server's start-up settings decisions without the database or a listener; print each refusal by setting name (never a value), warn when SWITCHBOARD_PUBLIC_URL does not answer /health"},
		}},
	{name: "up",
		summary: "Run this PC's hosting instance: the server with terminal hosting on, on 127.0.0.1:PORT with its database in DIR, and a node for this machine in the same process",
		options: []option{
			{"--data", "DIR", "data directory: database, node token, tmux config (created if missing)"},
			{"--port", "PORT", "loopback port to listen on"},
			{"--node", "NAME", "name of this machine's node, as host add --node names it (default local)"},
		}},
	{name: "node", args: "[invite NAME | statusline]", nargs: 2, optArgs: true,
		summary: "Run a node for this machine attached to a hosting instance (on loopback, or at its remote listener SWITCHBOARD_HOSTING_REMOTE_LISTEN); node invite NAME prints a one-time join code (15 min) from this instance's database (DB_PATH)",
		options: []option{
			{"--host", "URL", "the instance's URL, e.g. http://127.0.0.1:8090 or http://100.64.0.10:8091"}, // leak-guard:allow
			{"--join", "CODE", "exchange a code from node invite for this node's token, saved to --token-file"},
			{"--token-file", "PATH", "node token file (default node.token in the config directory; up writes DIR/node.token)"},
			{"--adapter", "NAME", "statusline: the session's agent adapter (the node sets it as the agent's status command)"},
			{"--out", "FILE", "statusline: where the usage report goes"},
			{"--restart-sessions", "", "stop every hosted session on this machine at start and relaunch each with resume"},
			{"--allow-checks", "", "run the prechecks of scheduled runs (host auto add --check) on this machine (default: refuse them; up's own node: SWITCHBOARD_HOSTING_ALLOW_CHECKS=on)"},
		}},
	{name: "admin", args: "ACTION [NAME [PERMS]]", nargs: 3, optArgs: true, synopsis: "(ACTION: create | passwd | totp-reset | disable | enable) NAME | allow NAME PERMS | list",
		summary: "Manage built-in dashboard admins in the server database (DB_PATH; run inside the server container); create and passwd read the password from the terminal or the first line of stdin; allow sets what a login may do on the work screen (PERMS: view,type,files,sessions,discard, or all, or none)",
		options: []option{optJSON}},
	{name: "version", summary: "Show build version; --check compares it with the server's minimum (exit 3 when behind)",
		options: []option{{"--check", "", "ask the server; print its version, this one and, when behind, the release asset to install"}, optProfile}},
}

var validStatuses = map[string]bool{"pending": true, "read": true, "acknowledged": true, "complete": true, "completed": true}

// profileFlag holds the --profile value given on the command line. It wins over SWITCHBOARD_PROFILE.
var profileFlag string

// ExitCodeError is an error from a command the binary supplies that exits
// with Code instead of ExitError.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e ExitCodeError) Error() string { return e.Err.Error() }
func (e ExitCodeError) Unwrap() error { return e.Err }

// UsageError is a mistake on the command line; it exits with ExitUsage.
type UsageError struct{ msg string }

func (e UsageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return UsageError{fmt.Sprintf(format, a...)} }

// Known reports whether a command line such as "host restart" or "jobs" is
// one this CLI takes: the first word a command, the second (when given) a
// word of its synopsis. The dashboard's CLI pairs test (#399) uses it.
func Known(line string) bool {
	words := strings.Fields(line)
	if len(words) == 0 {
		return false
	}
	c, ok := findCommand(words[0])
	if !ok || len(words) == 1 {
		return ok
	}
	text := " " + strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ", "|", " ", ":", " ").Replace(c.args+" "+c.synopsis) + " "
	return strings.Contains(text, " "+strings.Join(words[1:], " ")+" ")
}

func findCommand(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// HostFunc runs `host ACTION [ID]` against the server's database, which the
// client never opens, so the binary supplies it. add is the record for
// `host add`; id names the record for start, stop and remove.
type HostFunc func(action, id string, add wire.HostedSessionCreate, jsonOut bool, stdout io.Writer) (int, error)

// HostAutoFunc runs `host auto ACTION [REF]` against the server's database:
// REF is the hosted session for add and the scheduled run otherwise; runs is
// show's --runs.
type HostAutoFunc func(action, ref string, c wire.HostAutoCreate, edit wire.HostAutoEdit, runs int, jsonOut bool, stdout io.Writer) (int, error)

// AdminFunc runs `admin ACTION NAME [PERMS] [--json]`. Admin users live in
// the server's database, which the client never opens, so the binary
// supplies it. arg is the third word (allow's PERMS), "" when none.
type AdminFunc func(action, name, arg string, jsonOut bool, stdin io.Reader, stdout, stderr io.Writer) (int, error)

// ServeFunc runs `serve`: the server until stopped, or with check only its
// start-up settings check, returning the exit code.
type ServeFunc func(check bool) int

// UpFunc runs `up`: the server and a node in one process, until stopped.
type UpFunc func(dataDir string, port int, nodeName string) error

// NodeFunc runs `node`: a node attached to the host at hostURL, until
// stopped. A non-empty joinCode is first exchanged for the token written to
// tokenFile.
type NodeFunc func(hostURL, tokenFile, joinCode string, restartSessions, allowChecks bool) error

// NodeInviteFunc runs `node invite NAME` against the server's database.
type NodeInviteFunc func(name string, stdout io.Writer) (int, error)

// NodeStatusLineFunc runs `node statusline --adapter NAME --out FILE`: it
// reads the agent's status on stdin and prints the status line.
type NodeStatusLineFunc func(adapter, out string)

// PluginInstallFunc installs the agent plugins the binary carries (#477)
// under dir (this client's config folder) and writes one line per agent to
// w; quiet leaves out the agents where everything already matched.
type PluginInstallFunc func(version, dir string, quiet bool, w io.Writer) error

// PluginStatusFunc prints the carried plugins' state in the agents' settings.
// sync is the CLI's own record of keeping the plugin at its version (#483).
type PluginStatusFunc func(version, dir string, jsonOut bool, sync wire.PluginSync, w io.Writer) error

// Commands are the commands the binary supplies because they run the server
// or open its database.
type Commands struct {
	// PluginInstall and PluginStatus are `plugin install|status`; nil (a
	// build or test without them): no plugin is installed, ever.
	PluginInstall PluginInstallFunc
	PluginStatus  PluginStatusFunc
	Admin         AdminFunc
	Host          HostFunc
	HostAuto      HostAutoFunc
	HostGrant     HostGrantFunc
	HostGroup     HostGroupFunc
	Serve         ServeFunc
	Up            UpFunc
	Node          NodeFunc
	NodeInvite    NodeInviteFunc
	// NodeStatusLine is `node statusline` (#434).
	NodeStatusLine NodeStatusLineFunc
}

var (
	version  string       // build version, from Run
	admin    AdminFunc    // the admin command, from Run
	host     HostFunc     // the host command, from Run
	hostAuto HostAutoFunc // the host auto command, from Run
	binary   Commands     // every command the binary supplies, from Run
)

// Run runs the client command in args (the arguments after the program name)
// and returns the process exit code.
func Run(args []string, buildVersion string, cmds Commands) int {
	version, admin, host, hostAuto, hostGrant, hostGroup, binary = buildVersion, cmds.Admin, cmds.Host, cmds.HostAuto, cmds.HostGrant, cmds.HostGroup, cmds
	return runCLI(args)
}

// Usagef reports a UsageError (exit code 2) from a command the binary supplies.
func Usagef(format string, a ...any) error { return usagef(format, a...) }

func runCLI(args []string) int {
	code, err := dispatch(args)
	var ue UsageError
	if errors.As(err, &ue) {
		// The reason is the last line, where a reader of a long stderr looks.
		fmt.Fprintln(os.Stderr, "Run '"+progName()+" --help' for usage.")
		fmt.Fprintln(os.Stderr, progName()+": "+ue.msg)
		return ExitUsage
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, progName()+": "+err.Error())
		var ce ExitCodeError
		if errors.As(err, &ce) {
			return ce.Code
		}
		return ExitError
	}
	return code
}

func dispatch(args []string) (int, error) {
	// Global options before the command.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		a := args[0]
		switch {
		case a == "-h" || a == "--help":
			printHelp()
			return ExitOK, nil
		case a == "--profile":
			if len(args) < 2 {
				return 0, usagef("--profile requires a value")
			}
			profileFlag, args = args[1], args[2:]
		case strings.HasPrefix(a, "--profile="):
			profileFlag, args = strings.TrimPrefix(a, "--profile="), args[1:]
		case a == "--role":
			if len(args) < 2 {
				return 0, usagef("--role requires a value")
			}
			roleFlag, args = args[1], args[2:]
		case strings.HasPrefix(a, "--role="):
			roleFlag, args = strings.TrimPrefix(a, "--role="), args[1:]
		case a == "--no-plugin":
			noPluginFlag, args = true, args[1:]
		default:
			return 0, usagef("unknown option %s", a)
		}
	}
	if len(args) == 0 {
		return 0, usagef("missing command")
	}
	if args[0] == runnerCommand {
		// Hidden: the detached job runner `job run` starts (not in help).
		if len(args) != 2 {
			return 0, usagef("%s takes a job ID", runnerCommand)
		}
		c, err := loadConfig()
		if err != nil {
			return 0, err
		}
		return jobRunner(c, args[1])
	}
	name, rest := args[0], args[1:]
	if name == "help" {
		if len(rest) == 0 {
			printHelp()
			return ExitOK, nil
		}
		if _, ok := findCommand(rest[0]); !ok {
			return 0, usagef("unknown command %q", rest[0])
		}
		printCommandHelp(rest[0])
		return ExitOK, nil
	}
	cmd, ok := findCommand(name)
	if !ok {
		return 0, usagef("unknown command %q", name)
	}
	if hasHelp(rest) || len(rest) > 0 && rest[0] == "help" {
		printCommandHelp(name)
		return ExitOK, nil
	}
	p, err := parseCommandArgs(cmd, rest)
	if err != nil {
		return 0, usagef("%s: %s (see '%s %s --help')", name, err.Error(), progName(), name)
	}
	if name == "host" {
		// Its --profile names the hosted session's profile, not this
		// client's credentials. The plugin still follows the CLI's
		// version here, like any other command (#485).
		if !noAutoPlugin[name] {
			syncPluginOnce(os.Stderr)
		}
		return cliHost(p, os.Stdout)
	}
	if v, ok := p.values["--profile"]; ok {
		profileFlag = v
	}
	if profileFlag != "" && !validProfile(profileFlag) {
		return 0, usagef("invalid profile name %q", profileFlag)
	}
	if v, from := config.EnvAliasFrom(os.Getenv, "PROFILE"); profileFlag == "" && v != "" && !validProfile(v) {
		return 0, usagef("invalid profile name %q in %s", v, from)
	}
	if p.bool("--no-plugin") {
		noPluginFlag = true
	}
	if !noAutoPlugin[name] {
		syncPluginOnce(os.Stderr)
	}
	switch name {
	case "plugin":
		return cliPlugin(p, os.Stdout)
	case "version":
		if !p.bool("--check") {
			fmt.Println(progName() + " " + version)
			return ExitOK, nil
		}
		c, err := loadConfig()
		if err != nil {
			return 0, err
		}
		return versionCheck(c, version, os.Stdout)
	case "register":
		return cliRegister(p)
	case "admin":
		if len(p.pos) == 0 {
			return 0, usagef("admin: ACTION needed (create, passwd, totp-reset, disable, enable, allow, list)")
		}
		name, arg := "", ""
		if len(p.pos) > 1 {
			name = p.pos[1]
		}
		if len(p.pos) > 2 {
			arg = p.pos[2]
		}
		return admin(p.pos[0], name, arg, p.bool("--json"), os.Stdin, os.Stdout, os.Stderr)
	case "serve":
		if binary.Serve == nil {
			return 0, usagef("serve is not available in this build")
		}
		return binary.Serve(p.bool("--check")), nil
	case "up":
		return cliUp(p)
	case "node":
		return cliNode(p, os.Stdout)
	case "join":
		code, err := cliJoin(p, os.Stdout)
		if err == nil {
			// The profile is saved; the plugin follows (#477).
			installPluginAfterJoin(os.Stderr)
		}
		return code, err
	case "config":
		if p.pos[0] != "set" {
			return 0, usagef("config: unknown subcommand %q (only 'set')", p.pos[0])
		}
		return cliConfig(p)
	}
	if s := p.values["--status"]; s != "" && name != "request" && !validStatuses[s] { // request list checks its own (the server)
		return 0, usagef("%s: unknown status %q (pending, read, acknowledged, complete)", name, s)
	}
	if cmd.args == "ID" {
		if id, e := strconv.ParseInt(p.pos[0], 10, 64); e != nil || id <= 0 {
			return 0, usagef("%s: invalid message ID %q", name, p.pos[0])
		}
	}
	if name == "job" && p.pos[0] == "ping" {
		// ping reports a missing profile with its own exit code.
		return jobPing(p)
	}
	c, err := loadConfig()
	if err != nil {
		return 0, err
	}
	switch name {
	case "send":
		return cliSend(c, p)
	case "invite":
		return cliInvite(c, p, os.Stdout)
	case "inbox", "outbox":
		return cliList(c, name, p)
	case "wait":
		return cliWait(c, p)
	case "watch":
		if len(p.pos) == 1 && p.pos[0] == "list" && len(p.extra) == 0 {
			return cliWatchList(c, p)
		}
		if len(p.pos) > 0 || len(p.extra) > 0 {
			return watchStart(c, p)
		}
		return cliWatch(c, p)
	case "events":
		return cliEvents(c, p)
	case "slots":
		return cliSlots(c, p)
	case "slot":
		return cliSlot(c, p)
	case "status":
		return cliStatus(c, p)
	case "metrics":
		return cliMetrics(c, p)
	case "decisions":
		return cliDecisions(c, p)
	case "search":
		return cliSearch(c, p, os.Stdout)
	case "progress":
		return cliProgress(c, p)
	case "report":
		return cliReport(c, p, os.Stdout)
	case "request":
		return cliRequest(c, p, os.Stdout)
	case "rules":
		return cliRules(c, p, os.Stdout)
	case "job":
		return cliJob(c, p)
	case "schedule":
		return cliSchedule(c, p)
	case "mail":
		return cliMail(c, p)
	case "jobs":
		return cliJobs(c, p)
	case "hooks":
		return cliHooks(c, p, os.Stdin, os.Stdout)
	case "roles":
		return cliRoles(c, p)
	case "settings":
		return cliSettings(c, p)
	case "permission":
		return cliPermission(c, p)
	case "question":
		return cliQuestion(c, p)
	case "left":
		return cliLeft(c, p, os.Stdout)
	case "read", "ack", "complete":
		method, u := "GET", endpoint(c, "/api/messages/"+p.pos[0])
		moved := movedMessage(p.pos[0])
		if name != "read" {
			note, e := leftNoteOf(p)
			if e != nil {
				return 0, e
			}
			method, u = "POST", u+"/"+name
			req, _ := http.NewRequest(method, u, noteBody(note))
			if note != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			return doRequestWith(c, req, p.bool("--json"), moved)
		}
		req, _ := http.NewRequest(method, u, nil)
		if p.bool("--json") {
			return doRequestWith(c, req, true, both(moved, foreignNote()))
		}
		return doRequestWith(c, req, false, both(moved, readNote(c.Role)))
	case "close":
		note, e := leftNoteOf(p)
		if e != nil {
			return 0, e
		}
		if p.bool("--json") {
			req, _ := http.NewRequest("POST", endpoint(c, "/api/messages/"+p.pos[0]+"/close"), noteBody(note))
			if note != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			return doRequestWith(c, req, true, func(b []byte) {
				var out wire.Closed
				if json.Unmarshal(b, &out) == nil {
					noteMoved(p.pos[0], out.Asked())
				}
			})
		}
		var body any
		if note != nil {
			body = note
		}
		b, e := slotCall(c, "POST", "/api/messages/"+p.pos[0]+"/close", body)
		if e != nil {
			return 0, e
		}
		var out wire.Closed
		if e = json.Unmarshal(b, &out); e != nil {
			return 0, fmt.Errorf("unexpected response from server: %v", e)
		}
		noteMoved(p.pos[0], out.Asked())
		switch {
		case out.Completed != 0:
			fmt.Printf("#%d closed; #%d complete\n", out.ID, out.Completed)
		case out.Withdrawn:
			fmt.Printf("#%d closed; withdrawn (the recipient had not read it)\n", out.ID)
		default:
			fmt.Printf("#%d closed; the recipient already read it, so it stays in its mailbox marked as needing no answer\n", out.ID)
		}
		return ExitOK, nil
	case "withdraw":
		if p.bool("--json") {
			req, _ := http.NewRequest("POST", endpoint(c, "/api/messages/"+p.pos[0]+"/withdraw"), nil)
			return doRequestWith(c, req, true, func(b []byte) {
				var out wire.Withdrawn
				if json.Unmarshal(b, &out) == nil {
					noteMoved(p.pos[0], out.ID)
				}
			})
		}
		b, e := slotCall(c, "POST", "/api/messages/"+p.pos[0]+"/withdraw", nil)
		if e != nil {
			return 0, e
		}
		id := p.pos[0]
		var out wire.Withdrawn
		if json.Unmarshal(b, &out) == nil && out.ID != 0 {
			noteMoved(id, out.ID)
			id = strconv.FormatInt(out.ID, 10)
		}
		fmt.Printf("#%s withdrawn\n", id)
		return ExitOK, nil
	case "reply":
		body, e := inputBody(p)
		if e != nil {
			return 0, e
		}
		msg := withPriority(p, map[string]any{"subject": p.values["--subject"], "body": body, "recipient": p.values["--to"]})
		if p.bool("--directive") {
			// Omitted otherwise, like priority, so older servers accept the request.
			msg["directive"] = true
		}
		if p.bool("--complete") || p.bool("--done") {
			msg["done"] = true
		}
		if e := withLeft(p, msg); e != nil {
			return 0, e
		}
		if e := withRequestID(p, msg); e != nil {
			return 0, e
		}
		b, _ := json.Marshal(msg)
		req, _ := http.NewRequest("POST", endpoint(c, "/api/messages/"+p.pos[0]+"/reply"), bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if key := p.values["--idempotency-key"]; key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		return cliReply(c, req, p.pos[0], p.bool("--json"))
	}
	return 0, usagef("unknown command %q", name)
}

type parsedArgs struct {
	values map[string]string
	multi  map[string][]string // every value of an option given more than once, in order
	pos    []string
	extra  []string // after --, for a command with extra
}

func (p parsedArgs) bool(name string) bool { _, ok := p.values[name]; return ok }

// parseCommandArgs accepts only the options declared for cmd. Unknown options,
// missing values and a wrong number of positional arguments are errors.
func parseCommandArgs(cmd command, args []string) (parsedArgs, error) {
	p := parsedArgs{values: map[string]string{}, multi: map[string][]string{}}
	known := map[string]option{}
	for _, o := range cmd.options {
		known[o.name] = o
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" && cmd.extra {
			p.extra = append([]string{}, args[i+1:]...)
			break
		}
		if a == "--" {
			p.pos = append(p.pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			p.pos = append(p.pos, a)
			continue
		}
		name, value, hasValue := strings.Cut(a, "=")
		o, ok := known[name]
		if !ok {
			return p, fmt.Errorf("unknown option %s", name)
		}
		if o.arg == "" {
			if hasValue {
				return p, fmt.Errorf("option %s takes no value", name)
			}
			p.values[name] = ""
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return p, fmt.Errorf("option %s requires %s", name, o.arg)
			}
			i++
			value = args[i]
		}
		p.values[name] = value
		p.multi[name] = append(p.multi[name], value)
	}
	if len(p.pos) != cmd.nargs && !(cmd.optArgs && len(p.pos) < cmd.nargs) {
		if cmd.nargs == 0 {
			return p, fmt.Errorf("unexpected argument %q", p.pos[0])
		}
		return p, fmt.Errorf("expected %s, got %d argument(s)", cmd.args, len(p.pos))
	}
	return p, nil
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func printHelp() {
	var b strings.Builder
	b.WriteString("PROG — role addressed durable mailbox\n\nUsage: PROG [--profile NAME] [--role ROLE] [--no-plugin] <command> [options]\n\nCommands:\n")
	for _, c := range commands {
		label := c.name
		if c.args != "" {
			label += " " + c.args
		}
		fmt.Fprintf(&b, "  %-12s %s\n", label, c.summary)
	}
	b.WriteString(`  help CMD     Show options for a command

Examples:
  PROG register --endpoint https://mail.example --role team/worker --token-file role.token --enrollment-token-file enrollment.token
  PROG --profile worker config set --endpoint https://mail.example --role team/worker --token-file role.token
  PROG --profile worker send --to team/lead --subject 'Done' --body 'Work is complete'
  PROG inbox --status pending --json
  PROG wait --timeout 1800
  PROG events --after 41 --timeout 25

Use 'PROG <command> --help' for command options.
--profile NAME may come before or after the command; it overrides SWITCHBOARD_PROFILE.
A named profile that has no saved config is an error.
--role ROLE (before the command) or SWITCHBOARD_ROLE acts as a numbered role of the
profile's role, e.g. team/worker-2 for a team/worker profile (--role wins over the env).
The first run of a new version installs the agent plugin this binary carries (see plugin);
--no-plugin or SWITCHBOARD_PLUGIN=off skips that.
Every SWITCHBOARD_* variable also answers to its old AGENT_MAIL_* name; the new name wins.

Exit codes: 0 success, 1 error, 2 usage error (unknown command or option), 3 wait or events found no mail.
`)
	fmt.Print(strings.ReplaceAll(b.String(), "PROG", progName()))
}

func printCommandHelp(name string) {
	c, ok := findCommand(name)
	if !ok {
		printHelp()
		return
	}
	usage := progName() + " " + c.name
	if c.args != "" {
		usage += " " + c.args
	}
	if c.synopsis != "" {
		usage += " " + c.synopsis
	}
	if len(c.options) > 0 {
		usage += " [options]"
	}
	fmt.Printf("Usage: %s\n\n", usage)
	if c.example != "" {
		fmt.Printf("%s\n\n", strings.ReplaceAll(c.example, "PROG", progName()))
	}
	fmt.Printf("%s.\n\nOptions:\n", c.summary)
	for _, o := range c.options {
		label := o.name
		if o.arg != "" {
			label += " " + o.arg
		}
		fmt.Printf("  %-28s %s\n", label, o.help)
	}
	fmt.Printf("  %-28s %s\n", "-h, --help", "show this help")
	if c.name == "wait" {
		fmt.Println("\nExit codes: 0 pending messages printed, 3 timed out with none, 1 error.")
	}
}

func configPath() string {
	filename := profileName() + ".json"
	if filename == "default.json" {
		filename = "config.json"
	}
	if d := config.EnvAlias("CONFIG_DIR"); d != "" {
		return filepath.Join(d, filename)
	}
	d, e := os.UserConfigDir()
	if e != nil {
		d = "."
	}
	return profilePath(d, filename)
}

// profilePath returns <config>/switchboard/<file>. When only the old
// <config>/<config.LegacyConfigDir>/ holds that profile, it is copied once into the new
// directory (0700 directory, 0600 file) and the old file is left in place;
// from then on only the new file is read and written. An existing new file
// is never overwritten. If the copy fails, the old file is used as is.
func profilePath(configDir, filename string) string {
	p := filepath.Join(configDir, config.ConfigDirName, filename)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	old := filepath.Join(configDir, config.LegacyConfigDir, filename)
	if _, err := os.Stat(old); err != nil {
		return p
	}
	if copyProfile(old, p) != nil {
		return old
	}
	return p
}

// copyProfile copies src to dst, owner-only, failing if dst exists.
func copyProfile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	if err = ownerfile.RestrictToUser(dst); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	if err = f.Close(); err != nil {
		os.Remove(dst)
	}
	return err
}

// explicitProfile reports whether the user named a profile (flag or environment).
func explicitProfile() bool { return profileName() != "default" }

func profileName() string {
	name := profileFlag
	if name == "" {
		name = config.EnvAlias("PROFILE")
	}
	if name == "" {
		return "default"
	}
	if !validProfile(name) {
		fatal("invalid profile name")
	}
	return name
}

func validProfile(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func readConfig() Config {
	b, e := os.ReadFile(configPath())
	var c Config
	if e == nil {
		_ = json.Unmarshal(b, &c)
	}
	if v := config.EnvAlias("ENDPOINT"); v != "" {
		c.Endpoint = v
	}
	// With a saved role and no SWITCHBOARD_TOKEN, SWITCHBOARD_ROLE picks a
	// numbered role of the saved one (see actingRole); otherwise it is the
	// role itself, as in environment-only setups.
	if v := config.EnvAlias("ROLE"); v != "" && (c.Role == "" || config.EnvAlias("TOKEN") != "") {
		c.Role = v
	}
	if v := config.EnvAlias("TOKEN"); v != "" {
		c.Token = v
	}
	return c
}

// loadConfig returns the active profile's configuration. A profile named on the
// command line or in SWITCHBOARD_PROFILE must exist; it never falls back to another profile.
func loadConfig() (Config, error) {
	p := configPath()
	if explicitProfile() {
		if _, err := os.Stat(p); err != nil {
			return Config{}, fmt.Errorf("profile %q not found (%s); create it with: %s --profile %s config set --endpoint URL --role ROLE --token-file PATH", profileName(), p, progName(), profileName())
		}
	}
	c := readConfig()
	if c.Endpoint == "" || c.Token == "" || c.Role == "" {
		return c, fmt.Errorf("profile %q is not configured (%s); run: %s config set --endpoint URL --role ROLE --token-file PATH", profileName(), p, progName())
	}
	role, err := actingRole(c.Role)
	if err != nil {
		return c, err
	}
	c.Role = role
	return c, nil
}

// roleFlag holds the global --role value. It wins over SWITCHBOARD_ROLE.
var roleFlag string

// actingRole is the X-Switchboard-Role to send: --role, else SWITCHBOARD_ROLE,
// else the profile's role. An override must be the profile role itself or a
// numbered role <base>-N under it, since the profile's token covers only those.
func actingRole(profileRole string) (string, error) {
	want, from := roleFlag, "--role"
	if want == "" && config.EnvAlias("TOKEN") == "" {
		want, from = config.EnvAliasFrom(os.Getenv, "ROLE")
	}
	if want == "" || want == profileRole {
		return profileRole, nil
	}
	base, _, ok := wire.SplitSlot(want)
	if ok {
		if pb, _, numbered := wire.SplitSlot(profileRole); base == profileRole || numbered && base == pb {
			return want, nil
		}
	}
	return "", usagef("%s %q must be %s or %s-N (the profile's role or one of its numbered roles)", from, want, profileRole, profileRole)
}

func cliConfig(p parsedArgs) (int, error) {
	c := readConfig()
	if v := p.values["--endpoint"]; v != "" {
		c.Endpoint = v
	}
	if v := p.values["--role"]; v != "" {
		c.Role = v
	}
	if f := p.values["--token-file"]; f != "" {
		b, e := os.ReadFile(f)
		if e != nil {
			return 0, errors.New("cannot read token file")
		}
		c.Token = strings.TrimSpace(string(b))
	}
	if c.Endpoint == "" || c.Role == "" || c.Token == "" {
		return 0, errors.New("endpoint, role and token required")
	}
	path := configPath()
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	b, _ := json.MarshalIndent(c, "", "  ")
	if ownerfile.Write(path, b) != nil {
		return 0, errors.New("cannot write config")
	}
	fmt.Println("Saved", path)
	return ExitOK, nil
}

func cliRegister(p parsedArgs) (int, error) {
	for _, k := range []string{"--endpoint", "--role", "--token-file", "--enrollment-token-file"} {
		if p.values[k] == "" {
			return 0, usagef("register requires --endpoint, --role, --token-file and --enrollment-token-file")
		}
	}
	if !wire.ValidRole(p.values["--role"]) {
		return 0, errors.New("invalid role name")
	}
	credential, err := os.ReadFile(p.values["--token-file"])
	if err != nil {
		return 0, errors.New("cannot read role credential")
	}
	enrollment, err := os.ReadFile(p.values["--enrollment-token-file"])
	if err != nil {
		return 0, errors.New("cannot read enrollment credential")
	}
	payload, _ := json.Marshal(map[string]string{"role": p.values["--role"], "credential": strings.TrimSpace(string(credential))})
	req, _ := http.NewRequest("POST", strings.TrimRight(p.values["--endpoint"], "/")+"/api/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(enrollment)))
	setVersionHeader(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, errors.New("registration request failed: " + err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	fmt.Print(string(b))
	return ExitOK, nil
}

// idList parses message IDs from the values of a repeatable option, each
// of which may list IDs separated by commas ("#" prefixes allowed).
func idList(name string, values []string) ([]int64, error) {
	var ids []int64
	for _, v := range values {
		for _, f := range strings.Split(v, ",") {
			f = strings.TrimPrefix(strings.TrimSpace(f), "#")
			if f == "" {
				continue
			}
			id, err := strconv.ParseInt(f, 10, 64)
			if err != nil || id <= 0 {
				return nil, usagef("%s takes message IDs, got %q", name, f)
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// cliReply sends a reply and prints it like doRequest. A reply that leaves
// its directive open (no --done) adds one line on stderr saying so (#269).
func cliReply(c Config, req *http.Request, id string, jsonMode bool) (int, error) {
	authorize(c, req)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return 0, e
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if code, ok := refusedAmbiguous(resp.StatusCode, b, jsonMode, os.Stdout, os.Stderr); ok {
		return code, nil
	}
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var out wire.Message
	if json.Unmarshal(b, &out) == nil {
		out.NoSession = false // a reply prints only the #354 lines
		if noteMoved(id, out.ReplyTo) {
			id = strconv.FormatInt(out.ReplyTo, 10)
		}
		for _, line := range RecipientNotes(out.Recipient, out) {
			fmt.Fprintln(os.Stderr, line)
		}
		if out.DirectiveState == "open" {
			fmt.Fprintln(os.Stderr, ReplyOpenNote(id))
		}
	}
	return printResponse(b, jsonMode), nil
}

// DirectiveOpenNote is the line read prints under the header of answer, an
// answer to directive (which the reader sent) while it is still open (#275).
func DirectiveOpenNote(directive, answer int64) string {
	return fmt.Sprintf("directive #%d still open: close %d closes it and completes #%d", directive, answer, answer)
}

// ReplyOpenNote is the stderr line of a reply that leaves its directive open.
func ReplyOpenNote(id string) string {
	return "directive #" + id + " stays open: complete " + id + " --left-none (or --left ITEM) ends it (or next time reply " + id + " --complete --left-none)"
}

// ReadNote is the stderr line of read for its recipient while the mail is
// not complete (#327).
func ReadNote(id int64) string {
	return fmt.Sprintf("reply %d --complete answers and completes; complete %d ends it without an answer; ending a directive says what is left: --left ITEM or --left-none", id, id)
}

// MovedNote is the stderr line of a command given mail's old ID when the
// server answered with the ID it has now: held mail gets a new ID when a
// session takes it (#392).
func MovedNote(asked string, now int64) string {
	return fmt.Sprintf("#%s → #%d(넘겨지며 번호가 바뀜)", asked, now)
}

// noteMoved prints MovedNote on stderr when now is known and is not asked.
func noteMoved(asked string, now int64) bool {
	if old, err := wire.ParseID(asked); err != nil || now == 0 || old == now {
		return false
	}
	fmt.Fprintln(os.Stderr, MovedNote(asked, now))
	return true
}

// movedMessage is the response hook of read, ack and complete: MovedNote when
// the message came back under another ID than asked.
func movedMessage(asked string) func([]byte) {
	return func(b []byte) {
		var m wire.Message
		if json.Unmarshal(b, &m) == nil {
			noteMoved(asked, m.ID)
		}
	}
}

// both runs response hooks a then b.
func both(a, b func([]byte)) func([]byte) {
	return func(x []byte) { a(x); b(x) }
}

// foreign reports whether m is another mailbox's mail (a boss view). The
// server decides (view_only); an older server sends no such field, so no
// line is printed (#365).
func foreign(m wire.Message) bool {
	return m.ID != 0 && m.ViewOnly
}

// foreignNote is readNote for --json: only the other-mailbox line.
func foreignNote() func([]byte) {
	return func(b []byte) {
		var m wire.Message
		if json.Unmarshal(b, &m) == nil && foreign(m) {
			fmt.Fprintln(os.Stderr, otherMailboxNote)
		}
	}
}

// readNote returns a hook for the read response: ReadNote on stderr when
// role received the mail, it is not complete and it is no answer to an
// open directive (that one has DirectiveOpenNote).
func readNote(role string) func([]byte) {
	return func(b []byte) {
		var m wire.Message
		if json.Unmarshal(b, &m) == nil && foreign(m) {
			fmt.Fprintln(os.Stderr, otherMailboxNote)
			return
		}
		if json.Unmarshal(b, &m) != nil || m.ID == 0 || m.Recipient != role || m.Status == "complete" || m.Status == "completed" {
			return
		}
		if m.AnswersDirective != nil && m.AnswersDirective.State == wire.DirectiveOpen {
			return
		}
		fmt.Fprintln(os.Stderr, ReadNote(m.ID))
	}
}

func endpoint(c Config, path string) string { return strings.TrimRight(c.Endpoint, "/") + path }

func cliSend(c Config, p parsedArgs) (int, error) {
	body, e := inputBody(p)
	if e != nil {
		return 0, e
	}
	msg := withPriority(p, map[string]any{"recipient": p.values["--to"], "subject": p.values["--subject"], "body": body, "task_id": p.values["--task"]})
	ids, e := idList("--complete", p.multi["--complete"])
	if e != nil {
		return 0, e
	}
	if len(ids) > 0 {
		// Omitted otherwise, so older servers accept the request unchanged.
		msg["complete"] = ids
	}
	if e := withLeft(p, msg); e != nil {
		return 0, e
	}
	if e := withRequestID(p, msg); e != nil {
		return 0, e
	}
	b, _ := json.Marshal(msg)
	req, _ := http.NewRequest("POST", endpoint(c, "/api/messages"), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if key := p.values["--idempotency-key"]; key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return doRequestWith(c, req, p.bool("--json"), noSessionNote(p.values["--to"]))
}

// noSessionNote returns a hook for the send response: the lines of
// RecipientNotes on stderr (#300, #354).
func noSessionNote(to string) func([]byte) {
	return func(b []byte) {
		var m wire.Message
		if json.Unmarshal(b, &m) != nil {
			return
		}
		for _, line := range RecipientNotes(to, m) {
			fmt.Fprintln(os.Stderr, line)
		}
	}
}

// RecipientNotes are the lines a send or reply prints about the receiving
// mailbox: nothing when a session reads it; that its last session ended and
// the mail is held for it; the live numbered slots when slot 1 of a base is
// empty; else, from servers before #354 too, that no session of the role is
// attached.
func RecipientNotes(to string, m wire.Message) []string {
	st := m.RecipientState
	if st != nil && st.Live {
		return nil
	}
	var out []string
	if st != nil && st.Held {
		out = append(out, fmt.Sprintf("받는 세션 없음: %s, 마지막 세션 %d분 전 끝남. 붙잡아 둠", st.Mailbox, st.EndedS/60))
	}
	if st != nil && len(st.LiveSlots) > 0 {
		out = append(out, fmt.Sprintf("%s 이 살아 있음. 그 슬롯 이름으로 보내라", strings.Join(st.LiveSlots, ", ")))
	}
	if len(out) == 0 && m.NoSession {
		out = append(out, fmt.Sprintf("no session of %s is attached; the mail waits in its inbox", to))
	}
	return out
}

func listURL(c Config, box, q, status string) string {
	v := url.Values{"box": {box}}
	if q != "" {
		v.Set("q", q)
	}
	if status != "" {
		v.Set("status", status)
	}
	return endpoint(c, "/api/messages?"+v.Encode())
}

// withPriority adds "priority":"urgent" for --urgent and "no_reply":true for
// --no-reply. Without the flags the fields are omitted, so servers that
// predate them accept the request unchanged.
func withPriority(p parsedArgs, req map[string]any) map[string]any {
	if p.bool("--urgent") {
		req["priority"] = "urgent"
	}
	if p.bool("--no-reply") {
		// Omitted otherwise, so older servers accept the request unchanged.
		req["no_reply"] = true
	}
	return req
}

func cliList(c Config, box string, p parsedArgs) (int, error) {
	req, _ := http.NewRequest("GET", listURL(c, box, p.values["--q"], p.values["--status"]), nil)
	for _, k := range []string{"limit", "before"} {
		if v := p.values["--"+k]; v != "" {
			q := req.URL.Query()
			q.Set(k, v)
			req.URL.RawQuery = q.Encode()
		}
	}
	if of := p.values["--of"]; of != "" && box == "inbox" {
		q := req.URL.Query()
		q.Set("of", of)
		req.URL.RawQuery = q.Encode()
	}
	return doRequestWith(c, req, p.bool("--json"), listNote)
}

// listNote prints otherMailboxNote when the server marked the listed mail as
// another mailbox's view (#365).
func listNote(b []byte) {
	var ms []wire.Message
	if json.Unmarshal(b, &ms) == nil && len(ms) > 0 && ms[0].ViewOnly {
		fmt.Fprintln(os.Stderr, otherMailboxNote)
	}
}

// otherMailboxNote heads a read-only view of another mailbox.
const otherMailboxNote = "다른 메일함 보기(상태 안 바뀜)"

func inputBody(p parsedArgs) (string, error) {
	if f := p.values["--body-file"]; f != "" {
		b, e := os.ReadFile(f)
		return string(b), e
	}
	if p.values["--body"] != "" {
		return p.values["--body"], nil
	}
	st, e := os.Stdin.Stat()
	if e == nil && st.Mode()&os.ModeCharDevice == 0 {
		b, e := io.ReadAll(os.Stdin)
		return string(b), e
	}
	return "", errors.New("provide --body, --body-file or stdin")
}

func authorize(c Config, req *http.Request) {
	setVersionHeader(req)
	setLeftHeader(req)
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set(config.HeaderPrefix+"Role", c.Role)
}

func doRequest(c Config, req *http.Request, jsonMode bool) (int, error) {
	return doRequestWith(c, req, jsonMode, nil)
}

// doRequestWith is doRequest that also hands the response body to after.
func doRequestWith(c Config, req *http.Request, jsonMode bool, after func([]byte)) (int, error) {
	authorize(c, req)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return 0, e
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if code, ok := refusedAmbiguous(resp.StatusCode, b, jsonMode, os.Stdout, os.Stderr); ok {
		return code, nil
	}
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if after != nil {
		after(b)
	}
	return printResponse(b, jsonMode), nil
}

// refusedAmbiguous handles a 409 ambiguous_recipient answer to a send or
// reply (#364): "골라라" and the live slots on stderr, the body on stdout
// with jsonMode, and ExitError. ok is false for any other answer.
func refusedAmbiguous(status int, b []byte, jsonMode bool, stdout, stderr io.Writer) (int, bool) {
	var a wire.AmbiguousRecipient
	if status != http.StatusConflict || json.Unmarshal(b, &a) != nil || a.Error != wire.ErrAmbiguousRecipient {
		return 0, false
	}
	if jsonMode {
		var out bytes.Buffer
		if json.Indent(&out, b, "", "  ") == nil {
			b = out.Bytes()
		}
		fmt.Fprintln(stdout, string(b))
	}
	for _, line := range AmbiguousLines(a) {
		fmt.Fprintln(stderr, line)
	}
	return ExitError, true
}

// AmbiguousLines are the lines a refused send prints: which name to give
// instead, then one line per live slot (name, machine, folder, label).
func AmbiguousLines(a wire.AmbiguousRecipient) []string {
	out := []string{fmt.Sprintf("보내지 않음: %s 에 산 슬롯이 %d개다. 슬롯 이름으로 골라라 (1번은 %s-1)", a.Recipient, len(a.LiveSlots), a.Recipient)}
	for _, v := range a.LiveSlots {
		line := fmt.Sprintf("  %s  %s  %s", v.Role, v.Machine, v.Directory)
		if v.Label != "" {
			line += "  라벨 " + v.Label
		}
		out = append(out, line)
	}
	return out
}

// printResponse prints a response body: indented JSON with jsonMode, else
// for people.
func printResponse(b []byte, jsonMode bool) int {
	if jsonMode {
		var out bytes.Buffer
		if json.Indent(&out, b, "", "  ") == nil {
			b = out.Bytes()
		}
		fmt.Println(string(b))
		return ExitOK
	}
	var v any
	if json.Unmarshal(b, &v) == nil {
		pretty(v)
	} else {
		fmt.Println(string(b))
	}
	return ExitOK
}

func pretty(v any) { prettyTo(os.Stdout, v) }

// prettyTo prints messages for people. A header line starts with "#<id>";
// every body line is indented by two spaces, so a body line that itself
// starts with "#123" never reads as a header.
func prettyTo(w io.Writer, v any) {
	switch x := v.(type) {
	case []any:
		for _, y := range x {
			prettyTo(w, y)
			fmt.Fprintln(w)
		}
	case map[string]any:
		mark := ""
		if x["priority"] == "urgent" {
			mark = "[urgent] "
		}
		body, _ := x["body"].(string)
		if old, _ := x["held_id"].(float64); old != 0 && old != x["id"] {
			// held mail handed to a session got a new ID (#392)
			mark = fmt.Sprintf("(옛 #%d) ", int64(old)) + mark
		}
		repeat := ""
		if n, _ := x["alert_count"].(float64); n > 1 {
			last, _ := x["alert_last_at"].(string)
			t, _ := time.Parse(time.RFC3339Nano, last)
			repeat = " " + AlertRepeat(int(n), t)
		}
		fmt.Fprintf(w, "#%v %s%v%s\n%v → %v · %s\n", x["id"], mark, x["subject"], repeat, x["sender"], x["recipient"], x["status"])
		if ref, ok := x["answers_directive"].(map[string]any); ok && ref["state"] == wire.DirectiveOpen {
			id, _ := ref["id"].(float64)
			answer, _ := x["id"].(float64)
			if id != 0 && answer != 0 {
				fmt.Fprintln(w, DirectiveOpenNote(int64(id), int64(answer)))
			}
		}
		fmt.Fprintf(w, "%s\n", indentBody(body))
	default:
		fmt.Fprintln(w, v)
	}
}

// AlertRepeat is how a collapsing alert mail that came n times in all, the
// last at last, reads after its subject (#525): "×3 · 마지막 14:05" (local
// time; without a time only the count).
func AlertRepeat(n int, last time.Time) string {
	if last.IsZero() {
		return fmt.Sprintf("×%d", n)
	}
	return fmt.Sprintf("×%d · 마지막 %s", n, last.Local().Format("15:04"))
}

// indentBody prefixes every line of body with two spaces.
func indentBody(body string) string {
	return "  " + strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\n  ")
}

func secondsOption(cmd string, p parsedArgs, name string, def int, min int) (time.Duration, error) {
	raw, ok := p.values[name]
	if !ok {
		return time.Duration(def) * time.Second, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min {
		return 0, usagef("%s: %s must be a whole number of seconds >= %d", cmd, name, min)
	}
	return time.Duration(n) * time.Second, nil
}

// errPermanent marks request failures that retrying cannot fix (bad credentials, bad request).
type errPermanent struct{ error }

func fetchPending(client *http.Client, c Config) ([]wire.Message, error) {
	req, _ := http.NewRequest("GET", listURL(c, "inbox", "", "pending"), nil)
	authorize(c, req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		err := fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return nil, errPermanent{err}
		}
		return nil, err
	}
	var all []wire.Message
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, fmt.Errorf("unexpected response from server: %v", err)
	}
	pending := []wire.Message{}
	for _, m := range all {
		// Older servers ignore the status filter; filter here as well.
		if m.Status == "pending" {
			if m.Priority == "" { // servers that predate message priority
				m.Priority = "normal"
			}
			pending = append(pending, m)
		}
	}
	return pending, nil
}

// cliWait blocks until the active role has pending inbox messages, prints them and
// exits 0. It never changes message state. With no pending mail before the timeout it exits 3.
func cliWait(c Config, p parsedArgs) (int, error) {
	timeout, err := secondsOption("wait", p, "--timeout", 1800, 0)
	if err != nil {
		return 0, err
	}
	interval, err := secondsOption("wait", p, "--interval", 15, 1)
	if err != nil {
		return 0, err
	}
	jsonMode := p.bool("--json")
	client := &http.Client{Timeout: 30 * time.Second}
	deadline := time.Now().Add(timeout)
	fails := 0
	for {
		msgs, err := fetchPending(client, c)
		var perm errPermanent
		switch {
		case errors.As(err, &perm):
			return 0, perm.error
		case err != nil:
			fails++
			if fails >= 5 {
				return 0, fmt.Errorf("wait: giving up after %d failed checks: %v", fails, err)
			}
		case len(msgs) > 0:
			if jsonMode {
				b, _ := json.MarshalIndent(msgs, "", "  ")
				fmt.Println(string(b))
			} else {
				for _, m := range msgs {
					fmt.Println(mailLine(m.ID, m.Sender, m.Subject, m.TaskID, m.Priority))
				}
			}
			return ExitOK, nil
		default:
			fails = 0
		}
		sleep := interval
		if timeout > 0 {
			left := time.Until(deadline)
			if left <= 0 {
				if jsonMode {
					fmt.Println("[]")
				}
				fmt.Fprintf(os.Stderr, progName()+" wait: no pending messages after %s\n", timeout)
				return ExitTimeout, nil
			}
			if left < sleep {
				sleep = left
			}
		}
		time.Sleep(sleep)
	}
}

// mailLine is the one-line summary printed by wait and events:
// "#ID sender | subject[ | task=ID][ | urgent]".
func mailLine(id int64, sender, subject, task, priority string) string {
	line := fmt.Sprintf("#%d %s | %s", id, sender, oneLine(subject))
	if task != "" {
		line += " | task=" + task
	}
	if priority == "urgent" {
		line += " | urgent"
	}
	return line
}

// cliEvents makes one long-poll request to /api/events/poll. It exits 0 when
// events came back, 3 when the wait ended with none. It never changes message
// state; pass the printed cursor back with --after to resume.
func cliEvents(c Config, p parsedArgs) (int, error) {
	q := url.Values{}
	if raw, ok := p.values["--after"]; ok {
		if id, e := strconv.ParseInt(raw, 10, 64); e != nil || id < 0 {
			return 0, usagef("events: --after must be a message ID >= 0")
		}
		q.Set("after", raw)
	}
	wait, err := secondsOption("events", p, "--timeout", 25, 0)
	if err != nil {
		return 0, err
	}
	if _, ok := p.values["--timeout"]; ok {
		q.Set("timeout", strconv.FormatInt(int64(wait/time.Second), 10))
	}
	u := endpoint(c, "/api/events/poll")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	authorize(c, req)
	resp, err := (&http.Client{Timeout: wait + 30*time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var v struct {
		Events []wire.MailEvent `json:"events"`
		Cursor int64            `json:"cursor"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if p.bool("--json") {
		fmt.Println(strings.TrimSpace(string(b)))
	} else {
		for _, ev := range v.Events {
			fmt.Println(mailLine(ev.ID, ev.Sender, ev.Subject, ev.TaskID, ev.Priority))
		}
	}
	if len(v.Events) == 0 {
		fmt.Fprintf(os.Stderr, progName()+" events: no new mail (cursor %d)\n", v.Cursor)
		return ExitTimeout, nil
	}
	return ExitOK, nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func cliWatch(c Config, p parsedArgs) (int, error) {
	after, _ := wire.ParseID(p.values["--after"])
	jsonMode := p.bool("--json")
	for {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s?after=%d", endpoint(c, "/api/watch"), after), nil)
		authorize(c, req)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		var v struct {
			Messages []int64                      `json:"messages"`
			Cursor   int64                        `json:"cursor"`
			Answers  map[string]wire.DirectiveRef `json:"answers_directive"`
		}
		e = json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		if e != nil {
			time.Sleep(time.Second)
			continue
		}
		if v.Cursor > after {
			after = v.Cursor
		}
		for _, id := range v.Messages {
			ref, answers := v.Answers[strconv.FormatInt(id, 10)]
			if jsonMode {
				line := struct {
					MessageID int64              `json:"message_id"`
					Cursor    int64              `json:"cursor"`
					Answers   *wire.DirectiveRef `json:"answers_directive,omitempty"`
				}{MessageID: id, Cursor: after}
				if answers {
					line.Answers = &ref
				}
				b, _ := json.Marshal(line)
				fmt.Println(string(b))
			} else if answers && ref.State == wire.DirectiveOpen {
				fmt.Printf("New message #%d (cursor %d): answers directive #%d, still open\n", id, after, ref.ID)
			} else {
				fmt.Printf("New message #%d (cursor %d)\n", id, after)
			}
		}
	}
}

func fatal(s string) { fmt.Fprintln(os.Stderr, s); os.Exit(ExitError) }

// slotCall sends one slot API request and returns the response body.
func slotCall(c Config, method, path string, body any) ([]byte, error) {
	return slotCallKey(c, method, path, body, "")
}

// slotCallKey is slotCall with an Idempotency-Key header when key is set.
func slotCallKey(c Config, method, path string, body any, key string) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, endpoint(c, path), rd)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	authorize(c, req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func slotLine(v wire.SlotView) string {
	line := fmt.Sprintf("%s  %s:%s  until %s", v.Role, v.Machine, v.Directory, v.LeaseExpiresAt.Format(time.RFC3339))
	if v.Label != "" {
		line += "  label=" + v.Label
	}
	if v.Parent != "" {
		line += "  parent=" + v.Parent
	}
	return line
}

func cliSlots(c Config, p parsedArgs) (int, error) {
	b, err := slotCall(c, "GET", "/api/slots", nil)
	if err != nil {
		return 0, err
	}
	var out []wire.SlotView
	if err = json.Unmarshal(b, &out); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if p.bool("--json") {
		fmt.Println(strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	for _, v := range out {
		fmt.Println(slotLine(v))
	}
	return ExitOK, nil
}

func cliSlot(c Config, p parsedArgs) (int, error) {
	action := p.pos[0]
	switch action {
	case "register", "heartbeat", "takeover", "release":
	default:
		return 0, usagef("slot: unknown action %q (register, heartbeat, takeover, release)", action)
	}
	req := wire.SlotRequest{Machine: p.values["--machine"], Directory: p.values["--directory"], SessionID: p.values["--session-id"], Label: p.values["--label"], Parent: p.values["--parent"],
		State: p.values["--state"], LastTurnAt: p.values["--last-turn-at"]}
	if v, ok := p.values["--context-tokens"]; ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return 0, usagef("slot: --context-tokens must be a whole number of at least 0")
		}
		req.ContextTokens = &n
	}
	if v, ok := p.values["--subagents"]; ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return 0, usagef("slot: --subagents must be a whole number of at least 0")
		}
		req.Subagents = &n
	}
	if _, err := req.Presence(); err != nil {
		return 0, usagef("slot: %s", err.Error())
	}
	if req.Machine == "" {
		req.Machine, _ = os.Hostname()
	}
	if req.Directory == "" {
		req.Directory, _ = os.Getwd()
	}
	b, err := slotCall(c, "POST", "/api/slots/"+action, req)
	if err != nil {
		return 0, err
	}
	var g wire.SlotGrant
	if err = json.Unmarshal(b, &g); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if p.bool("--json") {
		fmt.Println(strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	fmt.Printf("%s (%d other active)\n", slotLine(g.SlotView), g.OtherActive)
	if g.Displaced != nil {
		fmt.Printf("moved previous holder: %s\n", slotLine(*g.Displaced))
	}
	return ExitOK, nil
}

// progName is the name the CLI was invoked as, for help and messages:
// the old name (config.LegacyConfigDir) when run under it, otherwise switchboard.
func progName() string {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if base == config.LegacyConfigDir {
		return config.LegacyConfigDir
	}
	return "switchboard"
}
