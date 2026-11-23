package dashboard

// CLIPair is one API the dashboard calls and the CLI command that does the
// same (#399). When a screen or an action is added to the dashboard, its
// call gets a row here; TestCLIPairs reads every call in dashboardHTML and
// fails on one without a row, on a command the CLI does not take, and on an
// exception without a reason.
//
// A boss role reaches the reads at /api/boss/<view>, /api/reports and the
// other role routes, and the writes at /api/boss/<admin path> (#399): a
// global boss as the admin, a group boss inside its groups.
type CLIPair struct {
	Method string // GET, POST, PUT, DELETE, or WS for a WebSocket
	Path   string // the path after /api/admin/, {} for a value, no query
	CLI    string // the command, e.g. "host restart"
	Why    string // instead of CLI: why there is no command
}

// The session APIs the dashboard does not call have their commands too
// (#408): GET rules/mine is `rules mine` and POST rules/hits `rules hit`
// (docs/rules-hints.md); the dashboard shows their result as each row's
// hit count and conditions in the 규칙 tab (GET rules, GET rules/{}).
// So do the ledger's other role APIs (#409): GET and POST rules/proposers
// are `rules proposer`, POST rules/classify `rules classify`; the 규칙 tab
// shows proposals (approve, reject) and the unclassified count.
//
// CLIPairs is the table. Keep it in the dashboard's order.
var CLIPairs = []CLIPair{
	{Method: "GET", Path: "me", Why: "the page's sign-in check and first-screen settings; the CLI signs in with the profile's role token"},
	{Method: "GET", Path: "roles", CLI: "roles"},
	{Method: "GET", Path: "presence", CLI: "status"},
	{Method: "GET", Path: "messages", CLI: "mail list"},
	{Method: "POST", Path: "messages/{}/close", CLI: "mail close"},
	{Method: "GET", Path: "metrics", CLI: "metrics"},
	{Method: "GET", Path: "progress", CLI: "progress"},
	{Method: "POST", Path: "left/{}/issue", CLI: "left issue"},
	{Method: "POST", Path: "left/{}/pass", CLI: "left pass"},
	{Method: "POST", Path: "left/{}/drop", Why: "dropping what is left is a signed-in admin's alone (#470); no role token may"},
	{Method: "GET", Path: "questions", CLI: "question list"},
	{Method: "POST", Path: "questions/{}/answer", CLI: "question answer"},
	{Method: "GET", Path: "decisions", CLI: "decisions"},
	{Method: "GET", Path: "rules", CLI: "rules list"},
	{Method: "GET", Path: "rules/topics", CLI: "rules topics"},
	{Method: "POST", Path: "rules", CLI: "rules add"},
	{Method: "POST", Path: "rules/{}/void", CLI: "rules void"},
	{Method: "GET", Path: "rules/unclassified", CLI: "rules unclassified"},
	{Method: "GET", Path: "rules/{}", CLI: "rules show"},
	{Method: "POST", Path: "rules/{}/approve", CLI: "rules approve"},
	{Method: "POST", Path: "rules/{}/reject", CLI: "rules reject"},
	{Method: "GET", Path: "reports", CLI: "report list"},
	{Method: "GET", Path: "reports/{}", CLI: "report show"},
	{Method: "GET", Path: "jobs", CLI: "jobs"},
	{Method: "GET", Path: "schedules", CLI: "jobs"},
	{Method: "GET", Path: "jobs/{}", CLI: "jobs show"},
	{Method: "GET", Path: "schedules/{}", CLI: "schedule show"},
	{Method: "POST", Path: "jobs/{}/stop", CLI: "jobs stop"},
	{Method: "DELETE", Path: "jobs/{}", CLI: "jobs rm"},
	{Method: "DELETE", Path: "schedules/{}", CLI: "jobs rm-schedule"},
	{Method: "POST", Path: "schedules/{}/pause", CLI: "schedule pause"},
	{Method: "POST", Path: "schedules/{}/resume", CLI: "schedule resume"},
	{Method: "GET", Path: "terminals/limit", CLI: "host limit"},
	{Method: "PUT", Path: "terminals/limit", CLI: "host limit"},
	{Method: "PUT", Path: "terminals/limit/accounts", CLI: "host limit"},
	{Method: "GET", Path: "terminals/work-groups", CLI: "host group"},
	{Method: "POST", Path: "terminals/work-groups", CLI: "host group"},
	{Method: "PUT", Path: "terminals/work-groups/{}", CLI: "host group"},
	{Method: "DELETE", Path: "terminals/work-groups/{}", CLI: "host group"},
	{Method: "GET", Path: "terminals", CLI: "host list"},
	{Method: "POST", Path: "terminals", CLI: "host add"},
	{Method: "PUT", Path: "terminals/{}", CLI: "host set"},
	{Method: "POST", Path: "terminals/{}/restart", CLI: "host restart"},
	{Method: "POST", Path: "terminals/{}/new-conversation", CLI: "host new-conversation"},
	{Method: "DELETE", Path: "terminals/{}", CLI: "host remove"},
	{Method: "GET", Path: "terminals/{}/events", CLI: "host events"},
	{Method: "GET", Path: "terminals/{}/log", CLI: "host log"},
	{Method: "GET", Path: "terminals/{}/autos", CLI: "host auto list"},
	{Method: "GET", Path: "terminals/{}/autos/{}", CLI: "host auto show"},
	{Method: "POST", Path: "terminals/{}/autos/{}/run", CLI: "host auto run"},
	{Method: "POST", Path: "terminals/{}/autos/{}/pause", CLI: "host auto pause"},
	{Method: "POST", Path: "terminals/{}/autos/{}/resume", CLI: "host auto resume"},
	{Method: "DELETE", Path: "terminals/{}/autos/{}", CLI: "host auto rm"},
	{Method: "POST", Path: "terminals/{}/ticket", Why: "a one-time ticket for the terminal view's socket, which only the page opens"},
	{Method: "WS", Path: "terminals/{}/socket", Why: "the live terminal view in the page (keystrokes and screen); the CLI reads the screen with host log"},
	{Method: "GET", Path: "question-settings", CLI: "settings"},
	{Method: "GET", Path: "question-refusals", CLI: "settings"},
	{Method: "PUT", Path: "question-settings/{}", CLI: "settings set"},
	{Method: "DELETE", Path: "question-settings/{}", CLI: "settings rm"},
	{Method: "POST", Path: "roles/{}/revoke", CLI: "roles revoke"},
	{Method: "POST", Path: "roles/{}/restore", CLI: "roles restore"},
	{Method: "GET", Path: "nodes", CLI: "host nodes"},
	{Method: "DELETE", Path: "nodes/{}", CLI: "host revoke-node"},
	{Method: "GET", Path: "host-grants", CLI: "host grant list"},
	{Method: "POST", Path: "host-grants", CLI: "host grant"},
	{Method: "DELETE", Path: "host-grants", CLI: "host revoke-grant"},
	{Method: "POST", Path: "node-invites", CLI: "node invite"},
	{Method: "GET", Path: "admin-actions", Why: "the admin's own action record (#446), read where the actions are taken: the 노드 index and a session's 기록"},
	{Method: "GET", Path: "settings", CLI: "settings"},
	{Method: "GET", Path: "permissions", CLI: "permission list"},
	{Method: "GET", Path: "permissions/{}", CLI: "permission show"},
	{Method: "POST", Path: "permissions/{}/answer", CLI: "permission answer"},
	{Method: "WS", Path: "live", Why: "the page's change bus, which tells open tabs to reload; a CLI command reads the state when it runs"},
}
