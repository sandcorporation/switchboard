package server

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// GET /api/messages pages with limit and before (#9): newest first, and the
// next page is what lies below the oldest ID shown.
func TestListPaging(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	var ids []int64
	for i := 0; i < 5; i++ {
		ids = append(ids, apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": fmt.Sprintf("m%d", i)}).ID)
	}
	list := func(query string) ([]wire.Message, int) {
		w := callAs(s, "GET", "/api/messages?box=inbox"+query, tokenFor("team/worker"), "team/worker", nil)
		var ms []wire.Message
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &ms); err != nil {
				t.Fatal(err)
			}
		}
		return ms, w.Code
	}
	if ms, _ := list(""); len(ms) != 5 {
		t.Fatalf("default: %d messages", len(ms))
	}
	page1, _ := list("&limit=2")
	if len(page1) != 2 || page1[0].ID != ids[4] || page1[1].ID != ids[3] {
		t.Fatalf("page 1: %+v", page1)
	}
	page2, _ := list(fmt.Sprintf("&limit=2&before=%d", page1[1].ID))
	if len(page2) != 2 || page2[0].ID != ids[2] || page2[1].ID != ids[1] {
		t.Fatalf("page 2: %+v", page2)
	}
	page3, _ := list(fmt.Sprintf("&limit=2&before=%d", page2[1].ID))
	if len(page3) != 1 || page3[0].ID != ids[0] {
		t.Fatalf("page 3: %+v", page3)
	}
	for _, q := range []string{"&limit=0", "&limit=501", "&limit=x", "&before=0", "&before=x"} {
		if _, code := list(q); code != 400 {
			t.Fatalf("%s: %d, want 400", q, code)
		}
	}
}
