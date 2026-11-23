// Package secrets keeps secret values — a webhook's real destination
// address, a watched API's key — sealed in the database (#525). Each value is
// sealed with AES-256-GCM under the server's secrets key, with a fresh nonce
// per row and the value's name as associated data, so a row moved under
// another name does not open. Nothing reads a value back out through the API,
// the CLI or the page: answers carry only wire.SecretInfo.
//
// The key comes from SWITCHBOARD_SECRETS_KEY (32 bytes, base64); unset, a
// generated key is kept in KeyFileName next to the database (owner-only).
// A value the key no longer opens reads as ErrUnreadable ("register again");
// it never stops the server. No error, log line or String of this package
// carries a value or the key.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/ownerfile"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// KeyFileName is the generated key's file, in the database's folder.
const KeyFileName = "secrets.key"

// keySize is the AES-256 key length.
const keySize = 32

// hintMin is the shortest value (in characters) that shows a hint; hintLen
// is how many of its last characters the hint shows.
const (
	hintMin = 16
	hintLen = 4
)

var (
	// ErrNotFound: no value is stored under the name.
	ErrNotFound = errors.New("secret not set")
	// ErrUnreadable: a value is stored, but the current key does not open
	// it (the key changed); it must be registered again.
	ErrUnreadable = errors.New("secret cannot be opened with the current key; register it again")
	// errKeyForm is ParseKey's refusal; it never quotes the value.
	errKeyForm = errors.New("must be 32 bytes in base64, such as the output of `openssl rand -base64 32`")
)

// Key seals and opens secret values. Its String never shows the key.
type Key struct {
	aead cipher.AEAD
}

// String hides the key from %v and log lines.
func (k *Key) String() string { return "secrets.Key(redacted)" }

// GoString hides the key from %#v.
func (k *Key) GoString() string { return k.String() }

// ParseKey reads a key in base64 (standard or URL alphabet, padded or not).
// The error never quotes v.
func ParseKey(v string) ([]byte, error) {
	v = strings.TrimSpace(v)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := enc.DecodeString(v); err == nil && len(raw) == keySize {
			return raw, nil
		}
	}
	return nil, errKeyForm
}

// CheckSetting is the start-up check of SWITCHBOARD_SECRETS_KEY (serve
// --check and serve): unset is fine (the key file is used), set must parse.
// It returns the parsed key, nil when unset.
func CheckSetting(getenv func(string) string) ([]byte, error) {
	v, from := config.EnvFrom(getenv, config.SecretsKeyName)
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	raw, err := ParseKey(v)
	if err != nil {
		return nil, fmt.Errorf("%s %w", from, err)
	}
	return raw, nil
}

// NewKey makes a Key of 32 raw bytes.
func NewKey(raw []byte) (*Key, error) {
	if len(raw) != keySize {
		return nil, errKeyForm
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Key{aead: aead}, nil
}

// Load returns the server's key: raw when the setting gave one (from
// CheckSetting), else the key in dir/KeyFileName, generated and written
// owner-only when the file does not exist. fromFile reports the file was
// used, for the start line. A key file that is not a key is an error: it is
// never overwritten.
func Load(raw []byte, dir string) (k *Key, fromFile bool, err error) {
	if raw != nil {
		k, err = NewKey(raw)
		return k, false, err
	}
	path := filepath.Join(dir, KeyFileName)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		raw, err = ParseKey(string(data))
		if err != nil {
			return nil, true, fmt.Errorf("key file %s %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		raw = make([]byte, keySize)
		if _, err = rand.Read(raw); err != nil {
			return nil, true, err
		}
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return nil, true, err
		}
		if err = ownerfile.Write(path, []byte(base64.StdEncoding.EncodeToString(raw)+"\n")); err != nil {
			return nil, true, fmt.Errorf("key file %s: cannot write: %w", path, err)
		}
	default:
		return nil, true, fmt.Errorf("key file %s: cannot read: %w", path, err)
	}
	k, err = NewKey(raw)
	return k, true, err
}

// Execer runs a statement: *sql.DB or *sql.Tx.
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// Queryer reads one row: *sql.DB or *sql.Tx.
type Queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

// Put seals value under name, replacing what was there.
func (k *Key) Put(db Execer, name, value string, now time.Time) error {
	if name == "" {
		return errors.New("secret name is empty")
	}
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := k.aead.Seal(nil, nonce, []byte(value), []byte(name))
	_, err := db.Exec(`INSERT INTO secrets(name,nonce,sealed,hint,set_at) VALUES(?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET nonce=excluded.nonce, sealed=excluded.sealed, hint=excluded.hint, set_at=excluded.set_at`,
		name, nonce, sealed, hint(value), now.UTC().Format(time.RFC3339Nano))
	return err
}

// Open returns the value stored under name, for the server's own use (the
// forward of a webhook, the call of a watched API) — never for an answer.
// ErrNotFound: none stored; ErrUnreadable: the key does not open it.
func (k *Key) Open(db Queryer, name string) (string, error) {
	var nonce, sealed []byte
	err := db.QueryRow(`SELECT nonce,sealed FROM secrets WHERE name=?`, name).Scan(&nonce, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if len(nonce) != k.aead.NonceSize() {
		return "", ErrUnreadable
	}
	plain, err := k.aead.Open(nil, nonce, sealed, []byte(name))
	if err != nil {
		return "", ErrUnreadable
	}
	return string(plain), nil
}

// Info is what an answer may say about the value under name: whether one is
// set, when, its hint, and whether the current key still opens it.
func (k *Key) Info(db Queryer, name string) (wire.SecretInfo, error) {
	var nonce, sealed []byte
	var h, at string
	err := db.QueryRow(`SELECT nonce,sealed,hint,set_at FROM secrets WHERE name=?`, name).Scan(&nonce, &sealed, &h, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return wire.SecretInfo{}, nil
	}
	if err != nil {
		return wire.SecretInfo{}, err
	}
	info := wire.SecretInfo{Set: true, Hint: h}
	if t, perr := time.Parse(time.RFC3339Nano, at); perr == nil {
		info.SetAt = &t
	}
	if len(nonce) != k.aead.NonceSize() {
		info.Unreadable = true
	} else if _, oerr := k.aead.Open(nil, nonce, sealed, []byte(name)); oerr != nil {
		info.Unreadable = true
	}
	return info, nil
}

// Delete removes the value under name; a missing one is no error.
func Delete(db Execer, name string) error {
	_, err := db.Exec(`DELETE FROM secrets WHERE name=?`, name)
	return err
}

// hint is "…" and the last hintLen characters of a value of at least hintMin
// characters, else empty: a short value would show too much of itself.
func hint(value string) string {
	if utf8.RuneCountInString(value) < hintMin {
		return ""
	}
	r := []rune(value)
	return "…" + string(r[len(r)-hintLen:])
}
