package agent

import "time"

// Usage is what an agent account has left (#434, spec section 4), as one
// status report of a session showed it.
type Usage struct {
	// Account names the account among the adapter's on this node; "" is
	// the adapter's only (default) one.
	Account string
	// Known is false when the report carried no limit values (before the
	// first response, or an account the agent reports none for).
	Known bool
	// Remaining is the percentage left of the tightest window, 0 to 100.
	Remaining int
	// Reset is when that window fills again; zero when not known.
	Reset time.Time
}

// Reporter is an adapter whose agent can run a command with its status on
// stdin, from which the node reads the account's remaining usage. An
// adapter that cannot report does not implement it.
type Reporter interface {
	// ReportArgs returns own (the launch arguments the node adds before the
	// record's args) with what makes the agent run command for its status.
	// It returns own unchanged when args (the record's) already set it.
	ReportArgs(own, args []string, command string) []string
	// ReadUsage reads one status input; getenv is the environment the
	// agent ran the command in.
	ReadUsage(input []byte, getenv func(string) string) Usage
	// ChainedCommand is the status command the operator configured for the
	// agent, which ReportArgs replaced in this session; "" for none.
	// readFile reads the agent's own settings files; home is the user's
	// home directory.
	ChainedCommand(input []byte, getenv func(string) string, readFile func(string) ([]byte, error), home string) string
}
