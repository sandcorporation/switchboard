package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// `host add --class execute` of a v0.47.0 CLI still succeeds after the
// session classes went (#435): the host decodes strictly, so the class
// field is taken and dropped, never a 400. The same holds for a class the
// host no longer knows and for a change carrying one.
func TestOldCLIClassIsTakenAndDropped(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	nodeToken(t, s, "pc")
	for i, class := range []string{"execute", "lead", "review", "coordinator"} {
		got := adminCall(t, base, "POST", "/api/admin/terminals", cookie, true, mustJSON(t, v047Create{
			Node: "pc", Base: "a", Profile: "team-a", Directory: "/work/old" + string(rune('a'+i)), Class: class,
		}))
		var h wire.HostedSession
		if got.code != http.StatusCreated || json.Unmarshal([]byte(got.body), &h) != nil {
			t.Fatalf("host add --class %s = %d %s", class, got.code, got.body)
		}
		if h.Group != "" || !h.AcceptingWork || strings.Contains(got.body, `"class"`) {
			t.Fatalf("host add --class %s: %s", class, got.body)
		}
		review := "review"
		if got := adminCall(t, base, "PUT", "/api/admin/terminals/"+h.ID, cookie, true, mustJSON(t, v047Change{Class: &review})); got.code != http.StatusOK {
			t.Fatalf("host set --class review = %d %s", got.code, got.body)
		}
	}
}

// Work groups over the admin API (#435): add, list, threshold, a session
// in one, no removing it while the session is in it.
func TestWorkGroupsAPI(t *testing.T) {
	t.Parallel()
	s, base, cookie := signedInServer(t, true)
	nodeToken(t, s, "pc")
	for _, c := range []struct {
		body string
		code int
	}{
		{`{"name":"slow","threshold":20}`, 201},
		{`{"name":"fast"}`, 201},
		{`{"name":"slow"}`, 409},
		{`{"name":"x","threshold":101}`, 400},
		{`{"name":"a/b"}`, 400},
		{`{"name":"x","limit":3}`, 400},
	} {
		if got := adminCall(t, base, "POST", "/api/admin/terminals/work-groups", cookie, true, c.body); got.code != c.code {
			t.Errorf("POST %s = %d %s, want %d", c.body, got.code, got.body, c.code)
		}
	}
	h := createRecord(t, base, cookie, `{"node":"pc","base":"a","profile":"p","directory":"/work/g","group":"slow"}`)
	if h.Group != "slow" || !h.AcceptingWork {
		t.Fatalf("record in slow: %+v", h)
	}
	if got := adminCall(t, base, "POST", "/api/admin/terminals", cookie, true, `{"node":"pc","base":"a","profile":"p","directory":"/work/h","group":"none"}`); got.code != 400 {
		t.Errorf("record of a missing group = %d %s", got.code, got.body)
	}
	var list []wire.WorkGroup
	if got := adminCall(t, base, "GET", "/api/admin/terminals/work-groups", cookie, false, ""); got.code != 200 || json.Unmarshal([]byte(got.body), &list) != nil ||
		len(list) != 2 || list[0].Name != "fast" || list[0].Threshold != nil || list[1].Name != "slow" || *list[1].Threshold != 20 || list[1].Sessions != 1 {
		t.Fatalf("list = %s", got.body)
	}
	if got := adminCall(t, base, "PUT", "/api/admin/terminals/work-groups/slow", cookie, true, `{"threshold":35}`); got.code != 200 || !strings.Contains(got.body, `"threshold":35`) {
		t.Fatalf("PUT threshold = %d %s", got.code, got.body)
	}
	if got := adminCall(t, base, "PUT", "/api/admin/terminals/work-groups/none", cookie, true, `{"threshold":5}`); got.code != 404 {
		t.Errorf("PUT missing = %d %s", got.code, got.body)
	}
	if got := adminCall(t, base, "DELETE", "/api/admin/terminals/work-groups/slow", cookie, true, ""); got.code != 409 {
		t.Fatalf("DELETE in use = %d %s", got.code, got.body)
	}
	if got := adminCall(t, base, "PUT", "/api/admin/terminals/"+h.ID, cookie, true, `{"group":"fast"}`); got.code != 200 {
		t.Fatalf("move to fast = %d %s", got.code, got.body)
	}
	if got := adminCall(t, base, "DELETE", "/api/admin/terminals/work-groups/slow", cookie, true, ""); got.code != 200 {
		t.Fatalf("DELETE = %d %s", got.code, got.body)
	}
}
