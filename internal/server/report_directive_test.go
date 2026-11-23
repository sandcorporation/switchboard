package server

import (
	"fmt"
	"strings"
	"testing"
)

// A worker's notification under the task of an open directive is a report,
// not a directive (#480): its recipient completes it without saying what is
// left. The same send under another task is still a directive.
func TestReportIsNotADirective(t *testing.T) {
	t.Parallel()
	s, clock := slotsWithRoles(t)
	apiMail(t, s, clock, "hq/boss", "/api/messages", map[string]any{"recipient": "team/worker", "subject": "start", "task_id": "job-1"})
	report := apiMail(t, s, clock, "team/worker", "/api/messages", map[string]any{"recipient": "hq/boss", "subject": "PR opened", "task_id": "job-1"})
	other := apiMail(t, s, clock, "team/worker", "/api/messages", map[string]any{"recipient": "hq/boss", "subject": "unrelated", "task_id": "job-2"})

	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/complete", other.ID), "hq/boss", nil); w.Code != 400 || !strings.Contains(w.Body.String(), "--left-none") {
		t.Fatalf("complete of a directive under another task: %d %s", w.Code, w.Body)
	}
	if w := callNew(s, "POST", fmt.Sprintf("/api/messages/%d/complete", report.ID), "hq/boss", nil); w.Code != 200 {
		t.Fatalf("complete of a report: %d %s", w.Code, w.Body)
	}
	wantRows(t, s)
}
