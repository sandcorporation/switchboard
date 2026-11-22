# Repository guidance

- Keep role identities independent of agent session or terminal handles.
- Do not log bearer tokens or put credentials in source, images, or committed configuration.
- Preserve message durability, idempotent sends, and non-consuming inbox/watch behavior.
- Render user-provided content as text; do not inject it as HTML.
- Go changes: locally run `scripts/go-changed` (tests the changed packages and every package importing them; `--list` shows which) and leave `scripts/go test ./...` to CI (`scripts/go` uses docker when the host has no Go and keeps caches out of `/tmp`). GitHub Actions is the CI and release system.
- Keep deployment private and avoid modifying unrelated services.

## Bundle track

Several tickets of one issue ship as one pull request from a shared branch (`bundle/<issue>`):

- One commit per ticket, its ticket in the subject (`(#<issue> T<n>)`); push the branch after each and tick the ticket's line in the issue.
- A push to `bundle/*` runs no checks: CI runs on `pull_request` and on pushes to `work` only. Before each push run `scripts/go-changed`, `gofmt -l` and, for code that builds on Windows, `GOOS=windows scripts/go build ./...`.
- Open the pull request after the last ticket, with `Closes #<issue>`; CI then runs the full suite once for the bundle.

## Package map

- `cmd/switchboard` holds `main` only. The server is assembled in `internal/server` (settings, router, role auth, admin guard, sweep).
- Domain packages: `identity` (roles, slots, groups, invites), `adminauth`, `mail`, `presence`, `asks` (questions, decisions, permissions), `insight` (metrics, alerts, Discord), `dashboard` (admin page, collaborators view). Leaves: `cli`, `wire`, `store`, `config`, `live` (the dashboard change bus).
- Domain packages never import `server` or `dashboard`. Pass cross-domain effects in as functions instead of importing upward. Read env and header names through `internal/config`; do not hard-code a prefix.
- Full table and import rules: ARCHITECTURE.md, "Package map".
