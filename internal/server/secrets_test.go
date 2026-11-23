package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/secrets"
)

// Without SWITCHBOARD_SECRETS_KEY the server makes and uses a key file next
// to the database and says so in one line that holds no key (#525).
func TestLoadSecretsKeyFile(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	logf := func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }
	s := &Server{}
	if err := s.loadSecrets(dir, logf); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "key file in use") {
		t.Fatalf("log %q", lines)
	}
	data, err := os.ReadFile(filepath.Join(dir, secrets.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lines[0], strings.TrimSpace(string(data))) {
		t.Fatal("the log line holds the key")
	}
	// With the setting, no key file and no line.
	raw, err := secrets.ParseKey("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	lines = nil
	s = &Server{secretsRaw: raw}
	if err := s.loadSecrets(other, logf); err != nil || len(lines) != 0 {
		t.Fatalf("setting: %v %q", err, lines)
	}
	if _, err := os.Stat(filepath.Join(other, secrets.KeyFileName)); !os.IsNotExist(err) {
		t.Fatal("key file made while the setting is set")
	}
	if s.secretsKey() == nil {
		t.Fatal("no key")
	}
}

// A server built without run (tests) still seals and opens.
func TestSecretsKeyForTests(t *testing.T) {
	s := testServer(t)
	k := s.secretsKey()
	if k == nil || k != s.secretsKey() {
		t.Fatal("key not kept")
	}
	if err := k.Put(s.db, "hook:a", "https://hooks.example/a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Open(s.db, "hook:a"); err != nil || v != "https://hooks.example/a" {
		t.Fatalf("%q %v", v, err)
	}
}
