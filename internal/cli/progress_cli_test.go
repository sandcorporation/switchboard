package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func TestProgressDurations(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "방금"}, {-time.Minute, "방금"}, {90 * time.Second, "2분 전"},
		{59 * time.Minute, "59분 전"}, {90 * time.Minute, "1.5시간 전"}, {11 * time.Hour, "11시간 전"},
		{36 * time.Hour, "1.5일 전"}, {12 * 24 * time.Hour, "12일 전"},
	} {
		if got := relTime(now, now.Add(-c.ago)); got != c.want {
			t.Errorf("relTime(%v) = %q, want %q", c.ago, got, c.want)
		}
	}
	for dir, want := range map[string]string{
		"/home/u/workspace/switchboard": "…/workspace/switchboard", "/home/u/workspace/switchboard/": "…/workspace/switchboard",
		`C:\Users\u\workspace\web`: "…/workspace/web", "/srv/x": "/srv/x", "": "-",
	} {
		if got := shortDir(dir); got != want {
			t.Errorf("shortDir(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestProgressSessionShowsSubagents(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	ss := []wire.ServiceProgress{{Service: "api", Badge: "busy", Sessions: []wire.ProgressSession{
		{Role: "api-1", State: "idle", Directory: "srv", Subagents: 2},
		{Role: "api-2", State: "busy", Directory: "srv"},
	}}}
	out := formatProgress(ss, now)
	if !strings.Contains(out, "    api-1 idle srv · 서브 에이전트 2개 작업 중\n") || !strings.Contains(out, "    api-2 busy srv\n") {
		t.Fatalf("progress text:\n%s", out)
	}
}

// The 남긴 것 items (#470): a count in the service block and a list after,
// each with its owner's request; the ones under none follow under 지시 없음
// with the command that attaches one (#564).
func TestProgressLeft(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	out := formatProgress([]wire.ServiceProgress{{Service: "team", Badge: "idle", Left: 2}}, now) + formatLeft([]wire.LeftItem{
		{ID: 7, Directive: 3, Kind: wire.LeftKindItem, Text: "rename the files", RecordedBy: "team/worker", RecordedAt: now.Add(-2 * time.Hour)},
		{ID: 8, Directive: 4, Kind: wire.LeftKindUnrecorded, RecordedBy: "team/worker", RecordedAt: now, Request: 15},
	}, now)
	want := "\n남긴 것 2\n  #8 지시 15 메일 #4 team/worker 방금 — (적지 않음)\n  지시 없음 1 (left attach ID --request-id N, or left done|drop|pass)\n  #7 메일 #3 team/worker 2시간 전 — rename the files\n"
	for _, want := range []string{"  남긴 것 2\n", want} {
		if !strings.Contains(out, want) {
			t.Fatalf("progress text missing %q:\n%s", want, out)
		}
	}
	if formatLeft(nil, now) != "" {
		t.Fatal("no items, want no section")
	}
}
