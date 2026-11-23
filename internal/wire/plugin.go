package wire

// PluginSync is how far the CLI has kept the carried agent plugin at its own
// version in one config folder (#483): `plugin status` shows it, and its
// --json puts it on every agent's row as "sync".
type PluginSync struct {
	// Marked is the version in the marker: the last version whose install
	// succeeded ("" before the first).
	Marked string `json:"marked,omitempty"`
	// LastAttempt is the last install this folder tried, on its own or
	// asked for; nil before the first.
	LastAttempt *PluginAttempt `json:"last_attempt,omitempty"`
}

// PluginAttempt is one install of the plugin.
type PluginAttempt struct {
	Version string `json:"version"`
	// At is when it ran, RFC 3339.
	At string `json:"at"`
	OK bool   `json:"ok"`
	// Reason is the error of a failed install, else its line (what it
	// changed, or why nothing was installed); "" when everything matched.
	Reason string `json:"reason,omitempty"`
}
