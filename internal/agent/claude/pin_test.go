package claude_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/agent/claude"
)

func TestPinEnvArgs(t *testing.T) {
	a := claude.Adapter{}
	env := []agent.EnvVar{{Name: "SWITCHBOARD_PROFILE", Value: "hosted-a"}, {Name: "SWITCHBOARD_MACHINE", Value: "pc-1"}}
	pin := `"env":{"SWITCHBOARD_PROFILE":"hosted-a","SWITCHBOARD_MACHINE":"pc-1"}`
	files := map[string]string{"own.json": `{"env":"not an object","a":1}`, "list.json": `[1]`}
	read := func(name string) ([]byte, error) {
		if b, ok := files[name]; ok {
			return []byte(b), nil
		}
		return nil, errors.New("no such file")
	}
	skip := a.PermissionSkipArgs(nil)
	for _, c := range []struct {
		name              string
		own, args         []string
		env               []agent.EnvVar
		wantOwn, wantArgs []string
	}{
		{"alone", nil, []string{"--model", "x"}, env, []string{"--settings", "{" + pin + "}"}, []string{"--model", "x"}},
		{"joins the permission skip", skip, nil, env, []string{"--dangerously-skip-permissions", "--settings", `{"skipDangerousModePermissionPrompt":true,` + pin + "}"}, nil},
		{"nothing to pin", skip, nil, nil, skip, nil},
		{"the record's inline settings", skip, []string{"--settings", `{"env":{"SWITCHBOARD_PROFILE":"x"}}`}, env, skip,
			[]string{"--settings", `{"env":{"SWITCHBOARD_MACHINE":"pc-1","SWITCHBOARD_PROFILE":"hosted-a"}}`}},
		{"the last of the record's settings", nil, []string{"--settings", "{}", "--settings={\"b\":true}"}, env, nil,
			[]string{"--settings", "{}", `--settings={"b":true,"env":{"SWITCHBOARD_MACHINE":"pc-1","SWITCHBOARD_PROFILE":"hosted-a"}}`}},
		{"a file whose env is not an object", nil, []string{"--settings", "own.json"}, env, nil,
			[]string{"--settings", `{"a":1,"env":{"SWITCHBOARD_MACHINE":"pc-1","SWITCHBOARD_PROFILE":"hosted-a"}}`}},
		{"a file that is not an object stays", nil, []string{"--settings", "list.json"}, env, nil, []string{"--settings", "list.json"}},
		{"a file that is not there stays", nil, []string{"--settings", "none.json"}, env, nil, []string{"--settings", "none.json"}},
		{"no value", nil, []string{"--settings"}, env, []string{"--settings", "{" + pin + "}"}, []string{"--settings"}},
	} {
		own, args := a.PinEnvArgs(c.own, c.args, c.env, read)
		if !slices.Equal(own, c.wantOwn) || !slices.Equal(args, c.wantArgs) {
			t.Errorf("%s: %q %q, want %q %q", c.name, own, args, c.wantOwn, c.wantArgs)
		}
	}
	in := []string{"--settings", "{}"}
	a.PinEnvArgs(skip, in, env, read)
	if !slices.Equal(skip, a.PermissionSkipArgs(nil)) || !slices.Equal(in, []string{"--settings", "{}"}) {
		t.Error("PinEnvArgs changed its input")
	}
}
