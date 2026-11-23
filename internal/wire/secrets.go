package wire

import "time"

// SecretInfo is all an answer says about a stored secret value (#525): the
// value itself is never read back. Hint is a short trace of the value's end
// ("…abcd"), empty for short values; Unreadable means the server's secrets
// key no longer opens it, so it must be registered again.
type SecretInfo struct {
	Set        bool       `json:"set"`
	SetAt      *time.Time `json:"set_at,omitempty"`
	Hint       string     `json:"hint,omitempty"`
	Unreadable bool       `json:"unreadable,omitempty"`
}
