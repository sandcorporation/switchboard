package remind_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/remind"
	"github.com/sandcorporation/switchboard/internal/store"
)

// #586: the left items' time stored before (left_remind) moves over with its
// since; the other kinds start with their start values (mail off, requests
// a day) counted from their first use, and a change counts from itself.
func TestMigrationKeepsLeftTimeAndStartValues(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	set := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err = db.Exec(`DELETE FROM remind_settings; INSERT INTO left_remind(id,after_s,since) VALUES(1,3600,?)`, set.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if db, err = store.Open(path); err != nil { // the migration runs again
		t.Fatal(err)
	}
	defer db.Close()
	now := set.Add(48 * time.Hour)
	want := map[remind.Kind]remind.Setting{
		remind.Left:    {Kind: remind.Left, After: time.Hour, Since: set},
		remind.Mail:    {Kind: remind.Mail, After: 0, Since: now},
		remind.Request: {Kind: remind.Request, After: 24 * time.Hour, Since: now},
	}
	for _, k := range remind.Kinds {
		got, err := remind.Load(ctx, db, k, now)
		if err != nil || got != want[k] {
			t.Fatalf("%s: %+v %v, want %+v", k, got, err, want[k])
		}
	}
	later := now.Add(time.Hour)
	if got, _ := remind.Save(ctx, db, remind.Left, time.Hour, later); !got.Since.Equal(set) {
		t.Fatalf("the same value moved since to %v", got.Since)
	}
	if got, _ := remind.Save(ctx, db, remind.Mail, 2*time.Hour, later); got.After != 2*time.Hour || !got.Since.Equal(later) {
		t.Fatalf("a new value: %+v", got)
	}
	if !(remind.Setting{After: time.Hour, Since: later}).Due(later.Add(time.Hour), later.Add(-5*time.Hour)) {
		t.Fatal("not due one time after since")
	}
	if (remind.Setting{After: time.Hour, Since: later}).Due(later.Add(time.Hour), later.Add(time.Minute)) {
		t.Fatal("due before one time after the latest news")
	}
	if _, err := remind.Parse("30s"); err == nil {
		t.Fatal("30s accepted")
	}
}
