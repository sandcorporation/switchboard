package secrets

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
)

const (
	fakeURL = "https://hooks.example/t/0123456789abcdef-SECRETTAIL"
	fakeKey = "sk-fake-0000-1111-2222-WATCHKEY"
)

var now = time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)

func openDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mail.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func rawKey(b byte) []byte { return bytes.Repeat([]byte{b}, keySize) }

func mustKey(t *testing.T, b byte) *Key {
	t.Helper()
	k, err := NewKey(rawKey(b))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealAndOpen(t *testing.T) {
	db, _ := openDB(t)
	k := mustKey(t, 1)
	if err := k.Put(db, "hook:a", fakeURL, now); err != nil {
		t.Fatal(err)
	}
	if err := k.Put(db, "watch:b", fakeKey, now); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"hook:a": fakeURL, "watch:b": fakeKey} {
		got, err := k.Open(db, name)
		if err != nil || got != want {
			t.Fatalf("Open(%s) = %q, %v", name, got, err)
		}
	}
	if _, err := k.Open(db, "hook:none"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v, want ErrNotFound", err)
	}
	// Replacing gives a fresh nonce and the new value.
	var n1 []byte
	db.QueryRow(`SELECT nonce FROM secrets WHERE name='hook:a'`).Scan(&n1)
	if err := k.Put(db, "hook:a", fakeURL+"2", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var n2 []byte
	db.QueryRow(`SELECT nonce FROM secrets WHERE name='hook:a'`).Scan(&n2)
	if bytes.Equal(n1, n2) {
		t.Fatal("nonce reused on replace")
	}
	if got, _ := k.Open(db, "hook:a"); got != fakeURL+"2" {
		t.Fatalf("after replace %q", got)
	}
	if err := Delete(db, "hook:a"); err != nil {
		t.Fatal(err)
	}
	if info, err := k.Info(db, "hook:a"); err != nil || info.Set {
		t.Fatalf("after delete %+v %v", info, err)
	}
}

// Every row has its own nonce, and a row moved under another name does not
// open (the name is the associated data).
func TestNoncePerRowAndNameBound(t *testing.T) {
	db, _ := openDB(t)
	k := mustKey(t, 1)
	k.Put(db, "hook:a", fakeURL, now)
	k.Put(db, "hook:b", fakeURL, now)
	var na, nb, sa, sb []byte
	db.QueryRow(`SELECT nonce,sealed FROM secrets WHERE name='hook:a'`).Scan(&na, &sa)
	db.QueryRow(`SELECT nonce,sealed FROM secrets WHERE name='hook:b'`).Scan(&nb, &sb)
	if bytes.Equal(na, nb) || bytes.Equal(sa, sb) {
		t.Fatal("two rows share a nonce or a sealed value")
	}
	if _, err := db.Exec(`UPDATE secrets SET nonce=?, sealed=? WHERE name='hook:b'`, na, sa); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Open(db, "hook:b"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("moved row: %v, want ErrUnreadable", err)
	}
}

// A changed key reads stored values as "register again" — an error, never a
// panic — and Info says so.
func TestOtherKeyIsUnreadable(t *testing.T) {
	db, _ := openDB(t)
	mustKey(t, 1).Put(db, "watch:b", fakeKey, now)
	other := mustKey(t, 2)
	if _, err := other.Open(db, "watch:b"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("other key: %v", err)
	}
	info, err := other.Info(db, "watch:b")
	if err != nil || !info.Set || !info.Unreadable || info.SetAt == nil || !info.SetAt.Equal(now) {
		t.Fatalf("info %+v %v", info, err)
	}
	if info, _ := mustKey(t, 1).Info(db, "watch:b"); info.Unreadable {
		t.Fatal("the right key reads as unreadable")
	}
	// Registering again under the new key works.
	other.Put(db, "watch:b", fakeKey, now)
	if got, err := other.Open(db, "watch:b"); err != nil || got != fakeKey {
		t.Fatalf("after re-register %q %v", got, err)
	}
}

// The value never shows: not in the database file, not in Info (whose hint
// is at most its last four characters), not in errors or the key's String.
func TestValueDoesNotLeak(t *testing.T) {
	db, path := openDB(t)
	k := mustKey(t, 1)
	k.Put(db, "hook:a", fakeURL, now)
	k.Put(db, "watch:short", "short-key", now)
	if _, err := db.Exec(`PRAGMA wal_checkpoint(FULL)`); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, v := range []string{fakeURL, "hooks.example", "0123456789abcdef", "short-key"} {
			if bytes.Contains(data, []byte(v)) {
				t.Fatalf("%s holds %q in clear", filepath.Base(f), v)
			}
		}
	}
	info, _ := k.Info(db, "hook:a")
	js, _ := json.Marshal(info)
	if strings.Contains(string(js), "hooks.example") || strings.Contains(string(js), "SECRET") {
		t.Fatalf("info shows the value: %s", js)
	}
	if info.Hint != "…TAIL" {
		t.Fatalf("hint %q", info.Hint)
	}
	if info, _ := k.Info(db, "watch:short"); info.Hint != "" {
		t.Fatalf("short value hint %q", info.Hint)
	}
	_, err := mustKey(t, 2).Open(db, "hook:a")
	for _, s := range []string{err.Error(), fmt.Sprint(k), fmt.Sprintf("%#v", k), fmt.Sprintf("%+v", k)} {
		if strings.Contains(s, "hooks.example") || strings.Contains(s, "\x01\x01\x01") || strings.Contains(s, base64.StdEncoding.EncodeToString(rawKey(1))) {
			t.Fatalf("leak in %q", s)
		}
	}
}

func TestParseKeyAndSetting(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(rawKey(7))
	for _, v := range []string{good, strings.TrimRight(good, "="), base64.URLEncoding.EncodeToString(rawKey(7)), " " + good + "\n"} {
		if raw, err := ParseKey(v); err != nil || !bytes.Equal(raw, rawKey(7)) {
			t.Fatalf("ParseKey(%q): %v", v, err)
		}
	}
	short := base64.StdEncoding.EncodeToString(rawKey(7)[:16])
	for _, v := range []string{short, "not base64 !!", "secret-passphrase-value"} {
		_, err := ParseKey(v)
		if err == nil {
			t.Fatalf("ParseKey(%q) accepted", v)
		}
		if strings.Contains(err.Error(), v) {
			t.Fatalf("error quotes the value: %v", err)
		}
	}
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "SWITCHBOARD_SECRETS_KEY" {
				return v
			}
			return ""
		}
	}
	if raw, err := CheckSetting(env("")); raw != nil || err != nil {
		t.Fatalf("unset: %v %v", raw, err)
	}
	if raw, err := CheckSetting(env(good)); err != nil || !bytes.Equal(raw, rawKey(7)) {
		t.Fatalf("good: %v", err)
	}
	_, err := CheckSetting(env("secret-passphrase-value"))
	if err == nil || !strings.Contains(err.Error(), "SWITCHBOARD_SECRETS_KEY") || strings.Contains(err.Error(), "secret-passphrase-value") {
		t.Fatalf("bad: %v", err)
	}
}

// Without the setting, Load makes an owner-only key file once and keeps
// using it; a broken key file is refused, not replaced.
func TestLoadKeyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	k1, fromFile, err := Load(nil, dir)
	if err != nil || !fromFile {
		t.Fatalf("first load: %v %v", fromFile, err)
	}
	path := filepath.Join(dir, KeyFileName)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", st.Mode().Perm())
	}
	db, _ := openDB(t)
	k1.Put(db, "hook:a", fakeURL, now)
	k2, fromFile, err := Load(nil, dir)
	if err != nil || !fromFile {
		t.Fatalf("second load: %v", err)
	}
	if got, err := k2.Open(db, "hook:a"); err != nil || got != fakeURL {
		t.Fatalf("second key does not open the first's value: %v", err)
	}
	// The setting wins over the file.
	k3, fromFile, err := Load(rawKey(3), dir)
	if err != nil || fromFile {
		t.Fatalf("setting: %v %v", fromFile, err)
	}
	if _, err := k3.Open(db, "hook:a"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("setting key opened the file key's value: %v", err)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(nil, dir); err == nil {
		t.Fatal("broken key file accepted")
	}
	if data, _ := os.ReadFile(path); string(data) != "garbage" {
		t.Fatal("broken key file was replaced")
	}
}
