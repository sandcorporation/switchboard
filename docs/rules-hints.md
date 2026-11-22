# Rules in sessions

The rules ledger (`internal/rules`, README "Rules ledger") reaches a session two ways:

- A row with hit conditions (`match`) goes to the session's tool calls, through a hook of the agent plugin. The plugin lives outside this repository; this page is its contract with the server.
- A row without conditions goes to the end of a boss role's directive mail, added by the server (`SWITCHBOARD_RULES_FOOTER`, default on).

A row with a `device` (the mechanism that enforces it) goes to neither, and neither does a proposed row (a proposer's tip waiting for approval, README "Rules ledger: proposals and answer classes").

A third hook keeps the ledger fed: the Stop hook does not let a session end its turn while a person's answer to one of its panel questions is unclassified (below).

## The pre-tool-call hook

Before each tool call the hook:

1. Reads `GET /api/rules/mine` with the role's token and role header. It keeps the body and its `ETag`, and sends `If-None-Match: <etag>` the next time; `304 Not Modified` means the kept body still holds. It revalidates when the session starts and after a compact; between them, as often as it likes (a 304 has no body).
2. Matches the tool call against each row's `match`. Every condition given must hold:
   - `tool`: the tool name, exactly, or a pattern where `*` is any text (`mcp__*`).
   - `command`: a Go (RE2) regular expression found anywhere in the command text (a shell tool's command). A call without a command text does not meet it.
   - `path`: a glob of the file path the tool touches: `*` is any text but `/`, `**` any text, `**/` also no directory at all, `?` one character but `/`. Paths are matched as the tool gives them (usually absolute), so start a pattern with `**/` to match anywhere.
   A row without `match` is never hit by a tool call.
3. For each hit row, adds a line to the call's extra context: `결정 #N 참고: <summary>` for a decision, `요령 #N 참고: <summary>` for a tip.
4. Sends `POST /api/rules/hits` with `{"ids":[N,…]}`, the rows it just put in.

The hook never blocks the call; it only adds context. A row goes in once per session, and once more after a compact (the context it added may be gone): the hook keeps the numbers it put in for the session and clears them on a compact. Whether and how to repeat a row within a session is the plugin's choice. When the server cannot be reached, the hook uses the body it kept, or adds nothing.

The server's reference for the matching is `wire.RuleMatch.Hits` (`internal/wire/rules.go`), tested in `internal/wire/rules_test.go` and `internal/server/rules_hints_test.go`.

## GET /api/rules/mine

Any role, a global boss included, gets the rows in force that reach its own role (its group's `all` rows, its service's rows and its tags' rows), without a `device`, newest first. `summary` is the first line of the row's text.

```http
GET /api/rules/mine
Authorization: Bearer <token>
X-Switchboard-Role: team/worker
If-None-Match: "3f2a9c0d81b7e645"
```

```json
{"rules":[
  {"id":12,"kind":"decision","topic":"release","summary":"No force push to a shared branch","match":{"tool":"Bash","command":"git push .*--force(\\s|$)"}},
  {"id":9,"kind":"tip","topic":"db","summary":"Migrations only add columns","match":{"tool":"Edit*","path":"**/migrations/*.sql"}},
  {"id":7,"kind":"tip","topic":"ci","summary":"Rerun a failed job once before reading the log"}
]}
```

The response carries `ETag` and `Cache-Control: private, no-cache`; a matching `If-None-Match` is `304` with no body. The ETag is a hash of the body: it changes exactly when the caller's rows do.

CLI: `switchboard rules mine [--json]`.

## POST /api/rules/hits

```json
{"ids":[12,9]}
```

Answers `{"counted":[12,9]}`: one hit for each named row among the caller's `mine` rows. Numbers outside them and repeats in the same call are left out; up to 100 numbers per call. An admin and a global boss see `hits` and `last_hit_at` on each row (`rules show N`, the dashboard's 규칙 tab).

CLI: `switchboard rules hit 12,9 [--json]`.

## Directive footer

A boss role's directive (a new message, not a reply, not `no_reply`) ends with the recipient's rows in force that have no `device` and no `match`:

```text
<the directive's body>

---
결정 #3 release
결정 #5 배포
요령 4줄 (주제: ci, db) — switchboard rules list --kind tip
```

Each decision is a line; the tips are one line with their count and topics. The footer is part of the stored body: an idempotent retry returns the message as first sent. Replies in the thread get no footer. `SWITCHBOARD_RULES_FOOTER=off` turns it off.

## The Stop hook: unclassified answers

Every answer a person gives to a panel question (in the dashboard or the asking session's panel) starts unclassified for the role that asked. Answers given before this came in are not counted; answers a boss role gives are not counted either. At the end of each turn the plugin's Stop hook:

1. Reads `GET /api/rules/unclassified` with the role's token and role header, sending `If-None-Match` with the `ETag` it kept; `304` means the kept body still holds.
2. When `count` is 0, lets the turn end.
3. Otherwise blocks the stop (the same way it holds a turn for a missing report) with a reason that lists each answer, one per line: `<id> <header>`, and what to do: `switchboard rules classify <id> --rule N` when the answer became rule N (or add the row with `--source <id>`, which classifies it), or `--once` when it held for this job only.

When the server cannot be reached or answers an error, the hook lets the turn end: it never traps a session on its own failure. It blocks at most a few times in a row for the same answers (the plugin's choice) and then lets the turn end with a note, so a session that cannot classify is not held forever.

```http
GET /api/rules/unclassified
Authorization: Bearer <token>
X-Switchboard-Role: team/worker
If-None-Match: "9b1e04c2a7d3f580"
```

```json
{"count":2,"answers":[
  {"id":"q_3f2a9c0d81b7e645","asker":"team/worker","header":"배포","answered_at":"2026-11-23T09:00:00Z"},
  {"id":"q_81b7e6453f2a9c0d","asker":"team/worker","header":"캐시","answered_at":"2026-11-23T09:05:00Z"}
]}
```

The role's own answers only, oldest first. An admin and a global boss get every role's, with `roles` (`[{"role","count"}]`), and may narrow to one with `?role=`. The ETag is a hash of the body.

```http
POST /api/rules/classify

{"question":"q_3f2a9c0d81b7e645","rule":12}
{"question":"q_81b7e6453f2a9c0d","once":true}
```

Only the asking role (or an admin or a global boss) classifies its answer; another role's is 404. `rule` must be a row the caller reads (or one it proposed); exactly one of `rule` and `once`. A classified answer may be classified again. The answer comes back as `{"id","asker","header","answered_at","class":"rule"|"once","rule"?}`.

CLI: `switchboard rules unclassified [--role ROLE] [--json]`, `switchboard rules classify <id> --rule N|--once [--json]`.
