package mail

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
)

func alertDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertAlert(t *testing.T, db *sql.DB, a Alert, at time.Time) AlertResult {
	t.Helper()
	var r AlertResult
	if err := store.InTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		r, err = InsertAlert(tx, a, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

// Repeats inside the window fold into the first mail; outside it, from
// another source, with another subject or to another mailbox they are new
// mail (#525).
func TestAlertFolding(t *testing.T) {
	db := alertDB(t)
	t0 := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	base := Alert{To: "team/boss", Source: "watch:api", Subject: "api down", Body: "first"}
	first := insertAlert(t, db, base, t0)
	if !first.New || first.Count != 1 {
		t.Fatalf("first %+v", first)
	}
	second := base
	second.Body = "second"
	r := insertAlert(t, db, second, t0.Add(4*time.Minute))
	if r.New || r.ID != first.ID || r.Count != 2 {
		t.Fatalf("inside window %+v", r)
	}
	r = insertAlert(t, db, base, t0.Add(9*time.Minute))
	if r.New || r.Count != 3 {
		t.Fatalf("still inside window %+v", r)
	}
	s := &Service{db: db}
	m, err := s.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Body != "first" || m.AlertCount != 3 || m.AlertSource != "watch:api" || m.AlertLastAt == nil || !m.AlertLastAt.Equal(t0.Add(9*time.Minute)) {
		t.Fatalf("folded mail %+v", m)
	}
	for name, a := range map[string]Alert{
		"other source":    {To: base.To, Source: "hook:api", Subject: base.Subject},
		"other subject":   {To: base.To, Source: base.Source, Subject: "api slow"},
		"other recipient": {To: "team/worker", Source: base.Source, Subject: base.Subject},
	} {
		if r := insertAlert(t, db, a, t0.Add(5*time.Minute)); !r.New || r.ID == first.ID {
			t.Fatalf("%s folded: %+v", name, r)
		}
	}
	// The window counts from the mail: at 10 minutes a new mail starts.
	next := insertAlert(t, db, base, t0.Add(10*time.Minute))
	if !next.New || next.ID == first.ID {
		t.Fatalf("after window %+v", next)
	}
	if r := insertAlert(t, db, base, t0.Add(11*time.Minute)); r.New || r.ID != next.ID || r.Count != 2 {
		t.Fatalf("folds into the newer mail %+v", r)
	}
	// Plain mail carries no alert fields.
	if m, _ := s.Get(next.ID - 1); m.AlertCount != 0 || m.AlertLastAt != nil {
		t.Fatalf("other mail %+v", m)
	}
}

// Each source may set its own window.
func TestAlertWindowPerSource(t *testing.T) {
	db := alertDB(t)
	t0 := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	short := Alert{To: "team/boss", Source: "hook:a", Subject: "s", Window: time.Minute}
	long := Alert{To: "team/boss", Source: "watch:b", Subject: "s", Window: time.Hour}
	insertAlert(t, db, short, t0)
	insertAlert(t, db, long, t0)
	if r := insertAlert(t, db, short, t0.Add(2*time.Minute)); !r.New {
		t.Fatal("short window folded after it ended")
	}
	if r := insertAlert(t, db, long, t0.Add(30*time.Minute)); r.New {
		t.Fatal("long window did not fold")
	}
}

func TestAlertNeedsFields(t *testing.T) {
	db := alertDB(t)
	for _, a := range []Alert{{Source: "hook:a", Subject: "s"}, {To: "team/boss", Subject: "s"}, {To: "team/boss", Source: "hook:a"}} {
		err := store.InTx(context.Background(), db, func(tx *sql.Tx) error {
			_, err := InsertAlert(tx, a, time.Now())
			return err
		})
		if err == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
}

// SendAlert wakes the recipient's streams for a new mail only, and the
// pending feed (event stream, long poll) carries an urgent alert once.
func TestSendAlertWakesOnce(t *testing.T) {
	db := alertDB(t)
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	hub := &Hub{}
	s := &Service{db: db, hub: hub, clock: func() time.Time { return now }}
	wake, cancel := hub.Subscribe("team/boss")
	defer cancel()
	a := Alert{To: "team/boss", Source: "watch:api", Subject: "api down", Body: "b", Urgent: true}
	r, err := s.SendAlert(context.Background(), a)
	if err != nil || !r.New {
		t.Fatal(r, err)
	}
	select {
	case <-wake:
	default:
		t.Fatal("new alert woke nothing")
	}
	evs, err := s.pendingAfter("team/boss", 0)
	if err != nil || len(evs) != 1 || evs[0].Priority != "urgent" {
		t.Fatalf("first feed %+v %v", evs, err)
	}
	cursor := evs[0].ID
	for i := 0; i < 3; i++ {
		now = now.Add(time.Minute)
		if r, err = s.SendAlert(context.Background(), a); err != nil || r.New {
			t.Fatal(r, err)
		}
	}
	select {
	case <-wake:
		t.Fatal("a folded repeat woke the stream")
	default:
	}
	if evs, _ = s.pendingAfter("team/boss", cursor); len(evs) != 0 {
		t.Fatalf("repeat delivered again %+v", evs)
	}
}
