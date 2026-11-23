package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// A collapsing alert mail shows ×N and its last time after the subject in
// read/inbox and the boss mail list; other mail shows neither (#525).
func TestAlertRepeatShown(t *testing.T) {
	last := time.Date(2026, 11, 23, 9, 7, 0, 0, time.UTC)
	want := "×3 · 마지막 " + last.Local().Format("15:04")
	if got := AlertRepeat(3, last); got != want {
		t.Fatalf("AlertRepeat = %q", got)
	}
	if got := AlertRepeat(2, time.Time{}); got != "×2" {
		t.Fatalf("no time: %q", got)
	}
	var b strings.Builder
	prettyTo(&b, map[string]any{"id": 7.0, "subject": "api down", "sender": "switchboard/system", "recipient": "team/boss", "status": "pending", "priority": "urgent",
		"body": "status 503", "alert_count": 3.0, "alert_last_at": last.Format(time.RFC3339Nano)})
	if !strings.Contains(b.String(), "#7 [urgent] api down "+want+"\n") {
		t.Fatalf("pretty %q", b.String())
	}
	b.Reset()
	prettyTo(&b, map[string]any{"id": 8.0, "subject": "plain", "sender": "team/worker", "recipient": "team/boss", "status": "pending", "body": ""})
	if strings.Contains(b.String(), "×") {
		t.Fatalf("plain mail %q", b.String())
	}
	list := formatMailList([]wire.Message{{ID: 7, Status: "pending", Sender: "switchboard/system", Recipient: "team/boss", Subject: "api down", AlertCount: 3, AlertLastAt: &last}})
	if !strings.Contains(list, "api down "+want) {
		t.Fatalf("list %q", list)
	}
}
