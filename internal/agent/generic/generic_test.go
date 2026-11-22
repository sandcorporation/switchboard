package generic_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/agent/generic"
)

const file = `{"adapters": [{
  "name": "acme",
  "executable": "acme-agent",
  "new_args": ["--conversation", "{id}"],
  "resume_args": ["--reopen={id}"],
  "permission_skip_args": ["--no-confirm"],
  "drop_env": ["ACME_SESSION"],
  "limit_patterns": ["(?i)quota exhausted"]
}, {
  "name": "plain",
  "executable": "plain-agent",
  "new_args": []
}]}`

func parse(t *testing.T, s string) []*generic.Adapter {
	t.Helper()
	list, err := generic.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestConfiguredAdapterArguments(t *testing.T) {
	list := parse(t, file)
	acme, plain := list[0], list[1]
	if acme.Name() != "acme" || acme.Executable() != "acme-agent" {
		t.Fatalf("%s %s", acme.Name(), acme.Executable())
	}
	for _, c := range []struct {
		resume bool
		want   []string
	}{{false, []string{"--conversation", "c1"}}, {true, []string{"--reopen=c1"}}} {
		if got := acme.LaunchArgs("c1", c.resume); !slices.Equal(got, c.want) {
			t.Errorf("resume=%v: %q, want %q", c.resume, got, c.want)
		}
	}
	// Without resume_args every launch is a new conversation.
	if got := plain.LaunchArgs("c1", true); len(got) != 0 {
		t.Errorf("plain resume %q", got)
	}
	if plain.ResumeFailed(1, time.Second) {
		t.Error("an agent that cannot resume failed to resume")
	}
	if !acme.ResumeFailed(1, time.Second) || acme.ResumeFailed(0, time.Second) || acme.ResumeFailed(1, time.Minute) {
		t.Error("resume failure rule")
	}
	if got := acme.PermissionSkipArgs(nil); !slices.Equal(got, []string{"--no-confirm"}) {
		t.Errorf("skip %q", got)
	}
	if got := acme.PermissionSkipArgs([]string{"--no-confirm"}); got != nil {
		t.Errorf("skip twice %q", got)
	}
	refused := acme.RefusedArgs()
	if _, ok := refused["--conversation"]; !ok || len(refused) != 2 {
		t.Errorf("refused %v", refused)
	}
	if _, ok := refused["--reopen"]; !ok {
		t.Errorf("refused %v", refused)
	}
	if acme.RoleArgs() != nil || acme.Words() != nil {
		t.Error("role args or words")
	}
	if got := acme.SessionMarkers(); !slices.Equal(got, []string{"ACME_SESSION"}) {
		t.Errorf("markers %q", got)
	}
}

func TestConfiguredAdapterLimit(t *testing.T) {
	acme := parse(t, file)[0]
	at := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	hit, ok := acme.DetectLimit([]string{"working", "Quota exhausted, resets 3pm"}, at)
	if !ok || !strings.HasPrefix(hit.Text, "Quota exhausted") || !hit.Reset.Equal(time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("hit %+v %v", hit, ok)
	}
	if _, ok := acme.DetectLimit([]string{"usage limit reached"}, at); ok {
		t.Error("another agent's message matched")
	}
	if _, ok := parse(t, file)[1].DetectLimit([]string{"quota exhausted"}, at); ok {
		t.Error("an adapter without patterns matched")
	}
}

func TestConfiguredAdapterFileErrors(t *testing.T) {
	for body, want := range map[string]string{
		`{"adapters":[{"name":"a","executable":"x","new_args":[],"colour":1}]}`:                          "unknown field",
		`{"adapters":[{"name":"a","new_args":[]}]}`:                                                      "executable is required",
		`{"adapters":[{"executable":"x"}]}`:                                                              "name is required",
		`{"adapters":[{"name":"a","executable":"x","new_args":["--new"],"resume_args":["--again"]}]}`:    "resume_args must contain {id}",
		`{"adapters":[{"name":"a","executable":"x","new_args":["--new"],"resume_args":["--r","{id}"]}]}`: "new_args must give it",
		`{"adapters":[{"name":"a","executable":"x","limit_patterns":["("]}]}`:                            "limit_patterns",
		`{"adapters":[{"name":"a","executable":"x","drop_env":["A B"]}]}`:                                "drop_env",
		`{"adapters":[{"name":"a","executable":"x"},{"name":"a","executable":"y"}]}`:                     "appears twice",
	} {
		if _, err := generic.Parse(strings.NewReader(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", body, err, want)
		}
	}
}
