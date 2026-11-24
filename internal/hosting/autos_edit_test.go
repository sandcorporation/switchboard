package hosting

import (
	"errors"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// An edited scheduled run keeps its ID and run history, and an active one
// fires next at the new cron's first time (#516).
func TestEditAutoKeepsIDAndRunsAndMovesTheNextTime(t *testing.T) {
	s := newService(t)
	rec, err := s.Create(record())
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateAuto(rec.ID, wire.HostAutoCreate{Cron: "0 9 * * *", Prompt: "old prompt", Label: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.addAutoRun(wire.HostAutoRun{Auto: a.ID, At: s.clock().UnixMilli(), Result: wire.AutoFired}); err != nil {
		t.Fatal(err)
	}
	cronExpr, prompt := "30 18 * * 1", "new prompt"
	got, err := s.EditAuto(a.ID, wire.HostAutoEdit{Cron: &cronExpr, Prompt: &prompt})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := nextFire(cronExpr, a.TZ, s.clock())
	if got.ID != a.ID || got.Cron != cronExpr || got.Prompt != prompt || got.Label != "old" || got.TZ != a.TZ ||
		got.State != wire.HostAutoActive || got.NextFireAt == nil || *got.NextFireAt != *want || *got.NextFireAt == *a.NextFireAt {
		t.Fatalf("edited %+v, want next %d (was %d)", got, *want, *a.NextFireAt)
	}
	if runs, err := s.AutoRuns(a.ID, 10); err != nil || len(runs) != 1 || runs[0].Result != wire.AutoFired {
		t.Fatalf("runs after the edit %+v %v", runs, err)
	}
}

// A paused scheduled run stays paused through an edit; resume reads the new
// cron.
func TestEditAutoLeavesAPausedRunPaused(t *testing.T) {
	s := newService(t)
	rec, _ := s.Create(record())
	a, _ := s.CreateAuto(rec.ID, wire.HostAutoCreate{Cron: "0 9 * * *", Prompt: "p"})
	if _, err := s.PauseAuto(a.ID); err != nil {
		t.Fatal(err)
	}
	label := "renamed"
	got, err := s.EditAuto(a.ID, wire.HostAutoEdit{Label: &label})
	if err != nil || got.State != wire.HostAutoPaused || got.NextFireAt != nil || got.Label != label {
		t.Fatalf("edited paused %+v %v", got, err)
	}
}

func TestEditAutoRefusals(t *testing.T) {
	s := newService(t)
	rec, _ := s.Create(record())
	a, _ := s.CreateAuto(rec.ID, wire.HostAutoCreate{Cron: "0 9 * * *", Prompt: "p"})
	bad, empty, never := "nope", " ", "0 0 30 2 *"
	var invalid InvalidError
	for name, e := range map[string]wire.HostAutoEdit{
		"nothing":     {},
		"bad cron":    {Cron: &bad},
		"never fires": {Cron: &never},
		"no prompt":   {Prompt: &empty},
	} {
		if _, err := s.EditAuto(a.ID, e); !errors.As(err, &invalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.EditAuto(a.ID+99, wire.HostAutoEdit{Prompt: &bad}); !errors.Is(err, ErrNoSuchAuto) {
		t.Errorf("missing: %v", err)
	}
	if got, _ := s.GetAuto(a.ID); got.Cron != "0 9 * * *" || got.Prompt != "p" {
		t.Fatalf("a refused edit changed %+v", got)
	}
}
