package claude

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/sandcorporation/switchboard/internal/agent"
)

var _ agent.EnvPinner = Adapter{}

// PinEnvArgs puts env in the env of the session's --settings (#462). Claude
// Code applies the folder's settings (.claude/settings.local.json,
// .claude/settings.json) over the environment it was started with, and
// command-line settings over the folder's
// (https://code.claude.com/docs/en/settings.md, "Settings precedence"), so
// the identity the node hands the session survives a folder that sets its
// own. Claude Code reads one --settings, the last, so this joins the one
// that is used: the record's when its args carry one (inline JSON, or a
// file read with readFile and passed inline in its place; a file that cannot
// be read as a JSON object is left as it is), else the one own carries (the
// permission skip's and the status line's), else a new one in own.
func (Adapter) PinEnvArgs(own, args []string, env []agent.EnvVar, readFile func(string) ([]byte, error)) ([]string, []string) {
	own, args = slices.Clone(own), slices.Clone(args)
	if len(env) == 0 {
		return own, args
	}
	at, value, joined := -1, "", false
	for i := 0; i < len(args); i++ {
		name, v, eq := strings.Cut(args[i], "=")
		if name != settingsFlag {
			continue
		}
		if eq {
			at, value, joined = i, v, true
		} else if i+1 < len(args) {
			at, value, joined = i+1, args[i+1], false
			i++
		}
	}
	if at >= 0 {
		if merged, ok := mergeEnv(value, env, readFile); ok {
			if joined {
				args[at] = settingsFlag + "=" + merged
			} else {
				args[at] = merged
			}
		}
		return own, args
	}
	b, err := json.Marshal(struct {
		Env json.RawMessage `json:"env"`
	}{envObject(env)})
	if err != nil {
		return own, args
	}
	return joinSettings(own, string(b)), args
}

// envObject is env as a JSON object, in env's order.
func envObject(env []agent.EnvVar) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, v := range env {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(v.Name)
		val, _ := json.Marshal(v.Value)
		b.Write(k)
		b.WriteByte(':')
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// mergeEnv is the settings value (inline JSON, or a file read with readFile)
// with env set in its env object; ok is false when it is not a JSON object.
func mergeEnv(value string, env []agent.EnvVar, readFile func(string) ([]byte, error)) (string, bool) {
	raw := []byte(value)
	if !strings.HasPrefix(strings.TrimSpace(value), "{") {
		if readFile == nil {
			return "", false
		}
		b, err := readFile(value)
		if err != nil {
			return "", false
		}
		raw = b
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil || settings == nil {
		return "", false
	}
	vars := map[string]json.RawMessage{}
	if prior, ok := settings["env"]; ok && json.Unmarshal(prior, &vars) != nil || vars == nil {
		vars = map[string]json.RawMessage{}
	}
	for _, v := range env {
		vars[v.Name], _ = json.Marshal(v.Value)
	}
	settings["env"], _ = marshal(vars)
	out, err := marshal(settings)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// marshal is JSON without HTML escaping or a trailing newline.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// joinSettings adds the fields of obj (a JSON object) to the first inline
// --settings of own, or appends a --settings of obj.
func joinSettings(own []string, obj string) []string {
	out := slices.Clone(own)
	for i := 0; i+1 < len(out); i++ {
		if out[i] != settingsFlag || !strings.HasPrefix(out[i+1], "{") || !strings.HasSuffix(out[i+1], "}") {
			continue
		}
		body := strings.TrimSpace(strings.TrimSuffix(out[i+1], "}"))
		if body == "{" {
			out[i+1] = obj
		} else {
			out[i+1] = body + "," + obj[1:]
		}
		return out
	}
	return append(out, settingsFlag, obj)
}
