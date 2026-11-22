package server

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// A boss role's writes (#399): a role that is no boss gets 403, a group boss
// writes inside its groups only (403 outside, and on server-wide targets), a
// global boss writes as the dashboard admin; every write is recorded with
// role, method, path and target, and no request body reaches the record or
// the log. Not parallel: it swaps the log output.
func TestBossWriteScopeAndRecord(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	s, mails := bossMailServer(t)
	jobIDs := map[string]string{}
	for _, j := range []struct{ token, role string }{{partnerDevToken, partnerDevRole}, {"secret-a", "a"}} {
		w := callAs(s, "POST", "/api/jobs", j.token, j.role, map[string]any{"machine": "m1", "kind": "command", "label": "build", "command": "make", "cwd": "/w"})
		var job wire.Job
		_ = json.Unmarshal(w.Body.Bytes(), &job)
		jobIDs[j.role] = strconv.FormatInt(job.ID, 10)
	}
	write := func(token, role, method, path string, body any) int {
		t.Helper()
		return callAs(s, method, "/api/boss/"+path, token, role, body).Code
	}

	stopA, stopDev := "jobs/"+jobIDs["a"]+"/stop", "jobs/"+jobIDs[partnerDevRole]+"/stop"
	for _, c := range []struct {
		name, token, role, method, path string
		body                            any
		want                            int
	}{
		{"non-boss", "secret-a", "a", "POST", stopA, nil, 403},
		{"group worker", partnerDevToken, partnerDevRole, "POST", stopDev, nil, 403},
		{"group boss outside", partnerBossToken, partnerBossRole, "POST", stopA, nil, 403},
		{"group boss inside", partnerBossToken, partnerBossRole, "POST", stopDev, nil, 200},
		{"global boss", bossToken, internalBossRole, "POST", stopA, nil, 200},
		{"group boss limit", partnerBossToken, partnerBossRole, "PUT", "terminals/limit", map[string]bool{"limited": true}, 404},
		{"group boss default setting", partnerBossToken, partnerBossRole, "PUT", "question-settings/*", map[string]any{"can_ask": true}, 403},
		{"group boss group setting", partnerBossToken, partnerBossRole, "PUT", "question-settings/partner-a/*", map[string]any{"can_ask": true, "watchers": []string{}}, 200},
		{"group boss revoke outside", partnerBossToken, partnerBossRole, "POST", "roles/a/revoke", nil, 403},
		{"group boss close own mail", partnerBossToken, partnerBossRole, "POST", "messages/" + strconv.FormatInt(mails["pa"], 10) + "/close", nil, 200},
		{"group boss close foreign mail", partnerBossToken, partnerBossRole, "POST", "messages/" + strconv.FormatInt(mails["a-b"], 10) + "/close", nil, 403},
		{"unknown job", bossToken, internalBossRole, "POST", "jobs/99999/stop", nil, 404},
		{"admin-only route", bossToken, internalBossRole, "POST", "invites", nil, 405},
		{"global boss revoke", bossToken, internalBossRole, "POST", "roles/a/revoke", nil, 200},
		{"global boss restore", bossToken, internalBossRole, "POST", "roles/a/restore", nil, 200},
	} {
		if got := write(c.token, c.role, c.method, c.path, c.body); got != c.want {
			t.Fatalf("%s: %s %s = %d, want %d", c.name, c.method, c.path, got, c.want)
		}
	}
	var revoked int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM roles WHERE role='a' AND revoked_at IS NOT NULL`).Scan(&revoked)
	if revoked != 0 {
		t.Fatalf("role a still revoked after restore")
	}

	// Terminals: a group boss changes a session of its group, not another's,
	// and not the server-wide limit or a node.
	s.hosting = true
	if _, err := s.db.Exec(`INSERT INTO nodes(name,token_hash,created_at) VALUES('n1','h',1)`); err != nil {
		t.Fatal(err)
	}
	svc := s.hostingService()
	mine, err := svc.Create(wire.HostedSessionCreate{Node: "n1", Base: partnerDevRole, Profile: "p", Directory: "/w/a"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := svc.Create(wire.HostedSessionCreate{Node: "n1", Base: "a", Profile: "p", Directory: "/w/b"})
	if err != nil {
		t.Fatal(err)
	}
	const secret = "label-value-never-logged"
	if got := write(partnerBossToken, partnerBossRole, "PUT", "terminals/"+mine.ID, map[string]string{"label": secret}); got != 200 {
		t.Fatalf("group boss set own session: %d", got)
	}
	if got := write(partnerBossToken, partnerBossRole, "PUT", "terminals/"+theirs.ID, map[string]string{"label": secret}); got != 403 {
		t.Fatalf("group boss set foreign session: %d", got)
	}
	if got := write(partnerBossToken, partnerBossRole, "PUT", "terminals/limit", map[string]bool{"limited": true}); got != 403 {
		t.Fatalf("group boss limit: %d", got)
	}
	if got := write(partnerBossToken, partnerBossRole, "DELETE", "nodes/n1", nil); got != 403 {
		t.Fatalf("group boss node: %d", got)
	}
	if got := write(bossToken, internalBossRole, "PUT", "terminals/limit", map[string]bool{"limited": true}); got != 200 {
		t.Fatalf("global boss limit: %d", got)
	}
	if rec, _ := svc.Get(mine.ID); rec.Label != secret {
		t.Fatalf("label not set: %+v", rec)
	}

	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM boss_writes WHERE role=? AND method='POST' AND path=? AND target=? AND status=200`, partnerBossRole, stopDev, partnerDevRole).Scan(&n)
	if n != 1 {
		t.Fatalf("group boss stop not recorded")
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM boss_writes WHERE role=? AND path='terminals/limit' AND status=403`, partnerBossRole).Scan(&n)
	if n != 1 {
		t.Fatalf("refused write recorded: %d", n)
	}
	var rows strings.Builder
	r, err := s.db.Query(`SELECT role||method||path||target FROM boss_writes`)
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
		var line string
		_ = r.Scan(&line)
		rows.WriteString(line + "\n")
	}
	r.Close()
	if strings.Contains(rows.String(), secret) || strings.Contains(logged.String(), secret) {
		t.Fatalf("a request body reached the record or the log")
	}
	if !strings.Contains(logged.String(), "boss write: role="+partnerBossRole+" PUT terminals/"+mine.ID) {
		t.Fatalf("log line missing: %s", logged.String())
	}
}
