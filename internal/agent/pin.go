package agent

// EnvVar is one environment variable: a name and its value.
type EnvVar struct {
	Name, Value string
}

// EnvPinner is an adapter whose agent lets a folder's own settings replace
// variables of the process environment (#462): such an agent applies the
// folder settings' env over the environment it was started with, so a
// folder that names a profile would replace the identity a node hands its
// hosted session. An adapter whose agent has no such path does not implement
// it; the environment alone is enough there.
type EnvPinner interface {
	// PinEnvArgs returns own (the launch arguments the node adds before the
	// record's args) and args (the record's) with what keeps env's values
	// in the session over the folder's settings. readFile reads a file the
	// record's args name, relative to the session's folder. Neither input
	// slice is changed.
	PinEnvArgs(own, args []string, env []EnvVar, readFile func(string) ([]byte, error)) (outOwn, outArgs []string)
}
