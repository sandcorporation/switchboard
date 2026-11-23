package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// requestServer is reportServer with panel questions on and a clock that
// moves one second on every read, so links come after a done mark.
func requestServer(t *testing.T) *Server {
	t.Helper()
	s := reportServer(t)
	if _, err := s.db.Exec(`UPDATE question_settings SET can_ask=1 WHERE target='default'`); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	s.clock = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		at = at.Add(time.Second)
		return at
	}
	return s
}

func decodeRequest(t *testing.T, w *httptest.ResponseRecorder, want int) wire.RequestDetail {
	t.Helper()
	if w.Code != want {
		t.Fatalf("want %d, got %d %s", want, w.Code, w.Body)
	}
	var d wire.RequestDetail
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
	return d
}

func addRequest(t *testing.T, s *Server, title, text string) wire.RequestDetail {
	t.Helper()
	return decodeRequest(t, callAs(s, "POST", "/api/requests", reportBossToken, reportBossRole, map[string]any{"title": title, "text": text}), 201)
}

func bossRequest(t *testing.T, s *Server, id int64) wire.RequestDetail {
	t.Helper()
	return decodeRequest(t, callAs(s, "GET", "/api/requests/"+itoa(id), reportBossToken, reportBossRole, nil), 200)
}

func adminRequestDetail(t *testing.T, s *Server, id int64) wire.RequestDetail {
	t.Helper()
	return decodeRequest(t, adminRequest(s, "GET", "/api/admin/requests/"+itoa(id), "", nil), 200)
}

func requestStatuses(t *testing.T, s *Server, query string) map[int64]string {
	t.Helper()
	w := callAs(s, "GET", "/api/requests"+query, reportBossToken, reportBossRole, nil)
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	var l wire.RequestList
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	out := map[int64]string{}
	for _, r := range l.Requests {
		out[r.ID] = r.Status
	}
	return out
}

func TestRequestBossOnlyAndIdempotent(t *testing.T) {
	t.Parallel()
	s := requestServer(t)
	if w := callAs(s, "POST", "/api/requests", reportWorkerToken, reportWorkerRole, map[string]any{"title": "x", "text": "y"}); w.Code != 403 {
		t.Fatalf("worker add: %d", w.Code)
	}
	for _, body := range []map[string]any{{"title": "", "text": "y"}, {"title": "a\nb", "text": "y"}, {"title": "x", "text": " "}} {
		if w := callAs(s, "POST", "/api/requests", reportBossToken, reportBossRole, body); w.Code != 400 {
			t.Fatalf("%v: %d", body, w.Code)
		}
	}
	first := decodeRequest(t, callAsIdem(s, "POST", "/api/requests", reportBossToken, reportBossRole, map[string]any{"title": "배포 정리", "text": "배포 정리해줘"}, "k1"), 201)
	again := decodeRequest(t, callAsIdem(s, "POST", "/api/requests", reportBossToken, reportBossRole, map[string]any{"title": "배포 정리", "text": "배포 정리해줘"}, "k1"), 200)
	if first.ID == 0 || again.ID != first.ID || first.Status != wire.RequestProgress || first.Text != "배포 정리해줘" || first.Author != reportBossRole {
		t.Fatalf("first %+v again %+v", first.Request, again.Request)
	}
	if w := callAs(s, "GET", "/api/requests", reportWorkerToken, reportWorkerRole, nil); w.Code != 403 {
		t.Fatalf("worker list: %d", w.Code)
	}
	if w := callAs(s, "GET", "/api/requests?status=nope", reportBossToken, reportBossRole, nil); w.Code != 400 {
		t.Fatalf("bad status: %d", w.Code)
	}
}

// TestRequestStatusFlow walks one request through every status: linked mail
// and a report keep it in progress, an unanswered question waits for the
// owner, done waits for a check, a check folds it, news after done reopens
// it and done again needs a new check.
func TestRequestStatusFlow(t *testing.T) {
	t.Parallel()
	s := requestServer(t)
	rq := addRequest(t, s, "옛 이름", "옛 이름 다 바꿔")
	other := addRequest(t, s, "다른 일", "다른 일 해줘")
	id := rq.ID

	note := decodeRequest(t, callAs(s, "POST", "/api/requests/"+itoa(id)+"/notes", reportBossToken, reportBossRole, map[string]any{"text": "문서도 같이"}), 200)
	if len(note.NoteList) != 1 || note.NoteList[0].Text != "문서도 같이" || note.Notes != 1 {
		t.Fatalf("note: %+v", note)
	}

	w := callAs(s, "POST", "/api/messages", reportBossToken, reportBossRole, map[string]any{"recipient": reportWorkerRole, "subject": "이름 바꾸기", "body": "x", "request_id": id})
	if w.Code != 201 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	mustReport(t, s, map[string]any{"title": "절반 바꿈", "body": "b", "request_id": id})
	d := bossRequest(t, s, id)
	if d.Status != wire.RequestProgress || d.Mail != 1 || d.Reports != 1 || len(d.MailList) != 1 || d.MailList[0].Subject != "이름 바꾸기" {
		t.Fatalf("after mail and report: %+v", d)
	}
	if d.Last == nil || d.Last.Kind != wire.SearchReport || d.Last.Title != "절반 바꿈" || d.Last.ID != "R1" {
		t.Fatalf("last: %+v", d.Last)
	}

	w = callAs(s, "POST", "/api/questions", reportBossToken, reportBossRole, map[string]any{"questions": sampleQuestions(), "request_id": id})
	if w.Code != 201 {
		t.Fatalf("ask: %d %s", w.Code, w.Body)
	}
	q := decodeQuestion(t, w.Body.Bytes())
	d = bossRequest(t, s, id)
	if d.Status != wire.RequestWaiting || d.OpenQuestions != 1 || d.Last.Kind != wire.SearchQuestion || d.Last.Title != "배포" {
		t.Fatalf("after ask: %+v last %+v", d.Request, d.Last)
	}
	if st := requestStatuses(t, s, "?status=waiting"); len(st) != 1 || st[id] != wire.RequestWaiting {
		t.Fatalf("waiting filter: %v", st)
	}
	if aw := adminWrite(s, "POST", "/api/admin/questions/"+q.ID+"/answer", adminAnswerBody(t)); aw.Code != 200 {
		t.Fatalf("answer: %d %s", aw.Code, aw.Body)
	}
	d = adminRequestDetail(t, s, id)
	if d.Status != wire.RequestProgress || len(d.QuestionList) != 1 || !slices.Equal(d.QuestionList[0].Answers, []string{"예", "web · api · 천천히"}) {
		t.Fatalf("after answer: %+v", d)
	}

	// A check before done is refused.
	if cw := adminWrite(s, "POST", "/api/admin/requests/"+itoa(id)+"/check", "{}"); cw.Code != 409 {
		t.Fatalf("check before done: %d", cw.Code)
	}
	d = decodeRequest(t, callAs(s, "POST", "/api/requests/"+itoa(id)+"/done", reportBossToken, reportBossRole, map[string]any{"result": "남은 곳 0"}), 200)
	if d.Status != wire.RequestDone || d.Result != "남은 곳 0" || d.DoneAt == nil {
		t.Fatalf("done: %+v", d.Request)
	}
	d = decodeRequest(t, adminWrite(s, "POST", "/api/admin/requests/"+itoa(id)+"/check", "{}"), 200)
	if d.Status != wire.RequestChecked || d.CheckedBy == "" || d.CheckedAt == nil {
		t.Fatalf("check: %+v", d.Request)
	}
	if st := requestStatuses(t, s, "?status=open"); st[id] != "" || st[other.ID] != wire.RequestProgress {
		t.Fatalf("open filter: %v", st)
	}
	d = decodeRequest(t, adminWrite(s, "POST", "/api/admin/requests/"+itoa(id)+"/uncheck", "{}"), 200)
	if d.Status != wire.RequestDone || d.CheckedAt != nil {
		t.Fatalf("uncheck: %+v", d.Request)
	}
	adminWrite(s, "POST", "/api/admin/requests/"+itoa(id)+"/check", "{}")

	// News after done reopens it; done again starts a new check.
	mustReport(t, s, map[string]any{"title": "하나 더", "body": "b", "request_id": id})
	if d = bossRequest(t, s, id); d.Status != wire.RequestProgress {
		t.Fatalf("news after done: %+v", d.Request)
	}
	d = decodeRequest(t, callAs(s, "POST", "/api/requests/"+itoa(id)+"/done", reportBossToken, reportBossRole, map[string]any{"result": "다 됨"}), 200)
	if d.Status != wire.RequestDone || d.CheckedAt != nil || d.Result != "다 됨" {
		t.Fatalf("done again: %+v", d.Request)
	}
}

func TestRequestLinksRefuseUnknown(t *testing.T) {
	t.Parallel()
	s := requestServer(t)
	if w := callAs(s, "POST", "/api/messages", reportBossToken, reportBossRole, map[string]any{"recipient": reportWorkerRole, "subject": "s", "body": "b", "request_id": 9}); w.Code != 400 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	if code, _, raw := postReport(s, reportBossToken, reportBossRole, map[string]any{"title": "t", "body": "b", "request_id": 9}, ""); code != 400 {
		t.Fatalf("report: %d %s", code, raw)
	}
	if w := callAs(s, "POST", "/api/questions", reportBossToken, reportBossRole, map[string]any{"questions": sampleQuestions(), "request_id": 9}); w.Code != 400 {
		t.Fatalf("ask: %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", "/api/requests/9/done", reportBossToken, reportBossRole, map[string]any{"result": "r"}); w.Code != 404 {
		t.Fatalf("done unknown: %d", w.Code)
	}
	// A report without a request keeps its JSON (no request_id key).
	rp := mustReport(t, s, map[string]any{"title": "t", "body": "b", "request": "옛 요청 글"})
	w := callAs(s, "GET", "/api/reports/"+itoa(rp.ID), reportBossToken, reportBossRole, nil)
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if _, ok := raw["request_id"]; ok || string(raw["request"]) != `"옛 요청 글"` {
		t.Fatalf("old report shape: %s", w.Body)
	}
}

func TestRequestCheckOnlyByLogin(t *testing.T) {
	t.Parallel()
	s := requestServer(t)
	rq := addRequest(t, s, "t", "x")
	callAs(s, "POST", "/api/requests/"+itoa(rq.ID)+"/done", reportBossToken, reportBossRole, map[string]any{"result": "r"})
	// The boss role's write routes do not include a check.
	if w := callAs(s, "POST", "/api/boss/requests/"+itoa(rq.ID)+"/check", reportBossToken, reportBossRole, map[string]any{}); w.Code < 400 {
		t.Fatalf("boss check: %d", w.Code)
	}
	if w := callAs(s, "POST", "/api/requests/"+itoa(rq.ID)+"/check", reportBossToken, reportBossRole, map[string]any{}); w.Code != 404 {
		t.Fatalf("role check: %d", w.Code)
	}
	// Without the dashboard guard the write is refused.
	if w := adminRequest(s, "POST", "/api/admin/requests/"+itoa(rq.ID)+"/check", "{}", nil); w.Code != 403 {
		t.Fatalf("unguarded: %d", w.Code)
	}
	if d := adminRequestDetail(t, s, rq.ID); d.Status != wire.RequestDone {
		t.Fatalf("status: %s", d.Status)
	}
}

func TestRequestLiveKinds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		path  string
		admin bool
	}{{"requests", false}, {"requests/1/done", false}, {"reports", false}, {"messages", false}, {"questions", false}, {"requests/1/check", true}, {"questions/q_x/answer", true}} {
		if !slices.Contains(liveKinds(c.path, c.admin), "requests") {
			t.Fatalf("%s admin=%v: %v", c.path, c.admin, liveKinds(c.path, c.admin))
		}
	}
	s := requestServer(t)
	sub, _, cancel, _ := s.live.Subscribe("", 0)
	defer cancel()
	addRequest(t, s, "t", "x")
	if ev := <-sub.C; !slices.Contains(ev.Kinds, "requests") {
		t.Fatalf("event %+v", ev)
	}
}

func TestRequestMigrationIsAdditive(t *testing.T) {
	t.Parallel()
	s := requestServer(t)
	rp := mustReport(t, s, map[string]any{"title": "옛 보고", "body": "b"})
	if err := store.Init(s.db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	for _, table := range []string{"reports", "messages", "questions"} {
		if has, err := store.HasColumn(s.db, table, "request_id"); err != nil || !has {
			t.Fatalf("%s.request_id: %v %v", table, has, err)
		}
	}
	if d := reportDetail(t, s, rp.ID); d.Title != "옛 보고" || d.RequestID != 0 {
		t.Fatalf("old report: %+v", d)
	}
}

func TestRequestCLI(t *testing.T) {
	t.Parallel()
	h := newCLIHarnessWith(t, func(s *Server) { s.bossRoles = map[string]bool{"a": true} })
	if _, err := h.s.db.Exec(`UPDATE question_settings SET can_ask=1 WHERE target='default'`); err != nil {
		t.Fatal(err)
	}
	h.configure(nil, "a")
	if out := h.mustRun(nil, "request", "add", "--title", "배포 정리", "--text", "배포 정리해줘\n범위는 알아서", "--idem", "k1"); out != "1" {
		t.Fatalf("add printed %q", out)
	}
	if out := h.mustRun(nil, "request", "add", "--title", "배포 정리", "--text", "배포 정리해줘\n범위는 알아서", "--idem", "k1"); out != "1" {
		t.Fatalf("retry printed %q", out)
	}
	if out := h.mustRun(nil, "request", "note", "1", "--text", "문서도"); out != "1" {
		t.Fatalf("note printed %q", out)
	}
	h.mustRun(nil, "send", "--to", "b", "--subject", "정리 맡김", "--body", "x", "--request-id", "1")
	h.mustRun(nil, "report", "--title", "반쯤", "--body", "x", "--request-id", "#1")
	file := filepath.Join(h.dir, "q.json")
	qs, _ := json.Marshal(sampleQuestions())
	if err := os.WriteFile(file, qs, 0600); err != nil {
		t.Fatal(err)
	}
	h.mustRun(nil, "question", "ask", "--file", file, "--request-id", "1")
	var l wire.RequestList
	if err := json.Unmarshal([]byte(h.mustRun(nil, "request", "list", "--json")), &l); err != nil || len(l.Requests) != 1 {
		t.Fatalf("list --json: %v %+v", err, l)
	}
	if r := l.Requests[0]; r.Status != wire.RequestWaiting || r.Notes != 1 || r.Mail != 1 || r.Reports != 1 || r.Questions != 1 {
		t.Fatalf("listed %+v", r)
	}
	if list := h.mustRun(nil, "request", "list"); !strings.HasPrefix(list, "1 waiting ") || !strings.Contains(list, " 배포 정리 — ") || !strings.Contains(list, " question ") {
		t.Fatalf("list:\n%s", list)
	}
	if r := h.run(nil, "request", "done", "1"); r.code == 0 || !strings.Contains(r.stderr, "--result") {
		t.Fatalf("done without result: %+v", r)
	}
	h.mustRun(nil, "request", "done", "1", "--result", "남은 곳 0")
	show := h.mustRun(nil, "request", "show", "1")
	for _, want := range []string{"1 waiting ", "배포 정리해줘\n범위는 알아서", "\nnote ", "문서도", "questions\n", "reports\n  R1 ", "mail\n  #1 a → b "} {
		if !strings.Contains(show, want) {
			t.Fatalf("show lacks %q:\n%s", want, show)
		}
	}
	if r := h.run(nil, "send", "--to", "b", "--subject", "s", "--body", "x", "--request-id", "x"); r.code == 0 || !strings.Contains(r.stderr, "--request-id") {
		t.Fatalf("bad id: %+v", r)
	}
	if r := h.run(nil, "send", "--to", "b", "--subject", "s", "--body", "x", "--request-id", "7"); r.code == 0 || !strings.Contains(r.stderr, "unknown request 7") {
		t.Fatalf("unknown id: %+v", r)
	}
	if out := h.mustRun(nil, "request", "list", "--status", "checked"); out != "" {
		t.Fatalf("checked list: %q", out)
	}
	h.configure(nil, "b")
	if r := h.run(nil, "request", "add", "--title", "t", "--text", "x"); r.code == 0 || !strings.Contains(r.stderr, "403") {
		t.Fatalf("non-boss: %+v", r)
	}
	if help := h.mustRun(nil, "help", "request"); !strings.Contains(help, "--result") || !strings.Contains(help, "--title") {
		t.Fatalf("help: %s", help)
	}
}
