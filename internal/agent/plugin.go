package agent

import "context"

// PluginInstaller is an adapter whose agent takes a plugin this binary
// carries (#477): the plugin's files ship inside the binary, so the plugin's
// version is always the binary's. An adapter whose agent has no plugin does
// not implement it; callers skip it.
type PluginInstaller interface {
	// InstallPlugin unpacks the plugin under o.Dir, registers it with the
	// agent and enables it, and turns off an older copy of it installed
	// another way. With the agent not installed it does nothing and says so
	// in PluginReport.Skipped; that is not an error.
	InstallPlugin(ctx context.Context, o PluginOptions) (PluginReport, error)
	// PluginStatus reports what the agent has installed.
	PluginStatus(ctx context.Context, o PluginOptions) (PluginState, error)
}

// PluginOptions says where and for whom a plugin is installed.
type PluginOptions struct {
	// Version is the binary's version (a release tag like v1.2.3, or a
	// development build's name); the plugin is stamped with it.
	Version string
	// Dir is the folder the adapter owns for its plugin files: each version
	// is unpacked under it, and older versions stay for sessions still
	// holding them.
	Dir string
	// Env is the environment the agent's commands run with ("KEY=value"),
	// which names the agent's user settings folder; nil: this process's.
	Env []string
}

// PluginReport is what an install did.
type PluginReport struct {
	// Skipped is non-empty when nothing was done, and why (the agent is not
	// installed here).
	Skipped string
	// Changes are the changes made to the agent's settings, one short phrase
	// each; empty when everything already matched.
	Changes []string
	// Where names the agent settings that were changed (its user settings
	// folder).
	Where string
}

// PluginState is the plugin's state in the agent's settings
// (`plugin status --json`).
type PluginState struct {
	Agent string `json:"agent"`
	// Available: the agent's CLI was found.
	Available bool `json:"available"`
	// Where is the agent's user settings folder.
	Where string `json:"where,omitempty"`
	// Want is the version this binary installs.
	Want string `json:"want"`
	// Installed is the installed version ("" when not installed); Enabled
	// whether it is on in the user settings.
	Installed string `json:"installed,omitempty"`
	Enabled   bool   `json:"enabled"`
	// Legacy lists the older copies installed another way.
	Legacy []LegacyPlugin `json:"legacy"`
}

// LegacyPlugin is an older copy of the plugin installed another way.
type LegacyPlugin struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
	// Project is the project folder of a local or project scope install.
	Project string `json:"project,omitempty"`
	Version string `json:"version,omitempty"`
	Enabled bool   `json:"enabled"`
}
