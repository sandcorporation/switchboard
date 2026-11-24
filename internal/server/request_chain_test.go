package server

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

const (
	chainCoordRole  = "team/coord"
	chainCoordToken = "secret-team-coord"
)

// chainServer is requestServer with a coordinator between the boss role and
// the worker.
func chainServer(t *testing.T) *Server {
	t.Helper()
	s := requestServer(t)
	if _, err := seedRoles(s.db, `INSERT INTO roles(role,token,created_at) VALUES(?,?,'now')`, chainCoordRole, chainCoordToken); err != nil {
		t.Fatal(err)
	}
	return s
}

// chainSend sends mail as role and returns its ID.
func chainSend(t *testing.T, s *Server, token, role string, body map[string]any) int64 {
	t.Helper()
	w := callAs(s, "POST", "/api/messages", token, role, body)
	if w.Code != 201 {
		t.Fatalf("send %v: %d %s", body, w.Code, w.Body)
	}
	var m struct{ ID int64 }
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func chainReply(t *testing.T, s *Server, token, role string, id int64, body map[string]any) int64 {
	t.Helper()
	w := callAs(s, "POST", "/api/messages/"+itoa(id)+"/reply", token, role, body)
	if w.Code != 201 {
		t.Fatalf("reply %d: %d %s", id, w.Code, w.Body)
	}
	var m struct{ ID int64 }
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func requestMailIDs(t *testing.T, s *Server, id int64) []int64 {
	t.Helper()
	var ids []int64
	for _, m := range bossRequest(t, s, id).MailList {
		ids = append(ids, m.ID)
	}
	return ids
}

// TestRequestChainFollowsMailDown: the mail a coordinator sends its worker
// under the task of the request's mail, and the worker's reply, join the
// request on read (#564), as does the worker's own mail under that task;
// mail under another task, or with no task, does not.
func TestRequestChainFollowsMailDown(t *testing.T) {
	t.Parallel()
	s := chainServer(t)
	rq := addRequest(t, s, "이름 바꾸기", "옛 이름 다 바꿔")
	other := addRequest(t, s, "다른 일", "다른 일")

	toCoord := chainSend(t, s, reportBossToken, reportBossRole, map[string]any{"recipient": chainCoordRole, "subject": "이름", "body": "x", "task_id": "t-1", "request_id": rq.ID})
	toWorker := chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "착수", "body": "x", "task_id": "t-1"})
	answer := chainReply(t, s, reportWorkerToken, reportWorkerRole, toWorker, map[string]any{"body": "보는 중"})
	chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "딴 일", "body": "x", "task_id": "t-2"})
	// The worker's own report under the task it got joins too.
	report := chainSend(t, s, reportWorkerToken, reportWorkerRole, map[string]any{"recipient": chainCoordRole, "subject": "PR 열림", "body": "x", "task_id": "t-1"})
	chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "그냥", "body": "x"})
	// Mail sent with another request's number stays with that one.
	own := chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "다른", "body": "x", "task_id": "t-1", "request_id": other.ID})

	if got, want := requestMailIDs(t, s, rq.ID), []int64{toCoord, toWorker, answer, report}; !slices.Equal(got, want) {
		t.Fatalf("request mail %v, want %v", got, want)
	}
	if got := requestMailIDs(t, s, other.ID); !slices.Equal(got, []int64{own}) {
		t.Fatalf("other request mail %v", got)
	}
	if d := bossRequest(t, s, rq.ID); d.Mail != 4 {
		t.Fatalf("mail count %d", d.Mail)
	}
}

func requestRemaining(t *testing.T, s *Server, id int64) map[int64]int {
	t.Helper()
	w := callAs(s, "GET", "/api/requests", reportBossToken, reportBossRole, nil)
	var l wire.RequestList
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	out := map[int64]int{}
	for _, r := range l.Requests {
		out[r.ID] = r.Remaining
	}
	return out
}

func openKinds(d wire.RequestDetail) []string {
	var out []string
	for _, o := range d.RemainingList {
		out = append(out, o.Kind+":"+o.ID)
	}
	return out
}

// TestRequestRemainingWork: the request's detail lists its unanswered
// directive mail, open panel questions and open left items as one list,
// counts what ended, and the list shows how many are left (#564).
func TestRequestRemainingWork(t *testing.T) {
	t.Parallel()
	s := chainServer(t)
	rq := addRequest(t, s, "이름 바꾸기", "옛 이름 다 바꿔")

	toCoord := chainSend(t, s, reportBossToken, reportBossRole, map[string]any{"recipient": chainCoordRole, "subject": "이름", "body": "x", "task_id": "t-1", "request_id": rq.ID})
	toWorker := chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "착수", "body": "x", "task_id": "t-1"})
	chainReply(t, s, reportWorkerToken, reportWorkerRole, toWorker, map[string]any{"body": "끝", "done": true, "left": []string{"문서 고치기"}})
	mustReport(t, s, map[string]any{"title": "절반 바꿈", "body": "b", "request_id": rq.ID})
	w := callAs(s, "POST", "/api/questions", reportBossToken, reportBossRole, map[string]any{"questions": sampleQuestions(), "request_id": rq.ID})
	if w.Code != 201 {
		t.Fatalf("ask: %d %s", w.Code, w.Body)
	}
	q := decodeQuestion(t, w.Body.Bytes())

	var leftID int64
	if err := s.db.QueryRow(`SELECT id FROM left_items WHERE directive_id=? AND kind='item'`, toWorker).Scan(&leftID); err != nil {
		t.Fatal(err)
	}
	d := bossRequest(t, s, rq.ID)
	want := []string{"mail:" + itoa(toCoord), "left:" + itoa(leftID), "question:" + q.ID}
	if got := openKinds(d); !slices.Equal(got, want) {
		t.Fatalf("remaining %v, want %v", got, want)
	}
	if d.Remaining != 3 || d.Finished != (wire.RequestFinished{Reports: 1, Mail: 1, Left: 0}) {
		t.Fatalf("remaining %d finished %+v", d.Remaining, d.Finished)
	}
	if o := d.RemainingList[0]; o.Who != chainCoordRole || o.Title != "이름" || !strings.Contains(o.Close, "close "+itoa(toCoord)) {
		t.Fatalf("mail item %+v", o)
	}
	if o := d.RemainingList[1]; o.Title != "문서 고치기" || o.Who != reportWorkerRole || !strings.Contains(o.Close, "left done "+itoa(leftID)) {
		t.Fatalf("left item %+v", o)
	}
	if n := requestRemaining(t, s, rq.ID)[rq.ID]; n != 3 {
		t.Fatalf("list remaining %d", n)
	}

	// Ending each takes it off the list and counts it.
	if w := callAs(s, "POST", "/api/left/"+itoa(leftID)+"/done", chainCoordToken, chainCoordRole, map[string]any{}); w.Code != 200 {
		t.Fatalf("left done: %d %s", w.Code, w.Body)
	}
	if aw := adminWrite(s, "POST", "/api/admin/questions/"+q.ID+"/answer", adminAnswerBody(t)); aw.Code != 200 {
		t.Fatalf("answer: %d %s", aw.Code, aw.Body)
	}
	chainReply(t, s, chainCoordToken, chainCoordRole, toCoord, map[string]any{"body": "끝", "done": true, "left_none": true})
	d = adminRequestDetail(t, s, rq.ID)
	if len(d.RemainingList) != 0 || d.Remaining != 0 || d.Finished != (wire.RequestFinished{Reports: 1, Mail: 2, Left: 1}) {
		t.Fatalf("after ending: %+v %+v", d.RemainingList, d.Finished)
	}
	if n := requestRemaining(t, s, rq.ID)[rq.ID]; n != 0 {
		t.Fatalf("list remaining after %d", n)
	}
}

func progressLeft(t *testing.T, s *Server) map[string]wire.LeftItem {
	t.Helper()
	w := adminRequest(s, "GET", "/api/admin/progress", "", nil)
	if w.Code != 200 {
		t.Fatalf("progress: %d %s", w.Code, w.Body)
	}
	var r struct{ Left []wire.LeftItem }
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	out := map[string]wire.LeftItem{}
	for _, it := range r.Left {
		out[it.Text] = it
	}
	return out
}

// TestLeftCarriesRequest: a 남긴 것 row carries its owner's request — the
// request of its directive on the chain — and one under no request can be
// attached to one by the directive's sender, a boss role or the dashboard
// (#564).
func TestLeftCarriesRequest(t *testing.T) {
	t.Parallel()
	s := chainServer(t)
	rq := addRequest(t, s, "이름 바꾸기", "옛 이름 다 바꿔")
	other := addRequest(t, s, "다른 일", "다른 일")

	chainSend(t, s, reportBossToken, reportBossRole, map[string]any{"recipient": chainCoordRole, "subject": "이름", "body": "x", "task_id": "t-1", "request_id": rq.ID})
	toWorker := chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "착수", "body": "x", "task_id": "t-1"})
	chainReply(t, s, reportWorkerToken, reportWorkerRole, toWorker, map[string]any{"body": "끝", "done": true, "left": []string{"문서"}})
	loose := chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "따로", "body": "x"})
	chainReply(t, s, reportWorkerToken, reportWorkerRole, loose, map[string]any{"body": "끝", "done": true, "left": []string{"정리"}})

	left := progressLeft(t, s)
	if it := left["문서"]; it.Request != rq.ID || it.RequestTitle != "이름 바꾸기" {
		t.Fatalf("chained item %+v", it)
	}
	b := left["정리"]
	if b.Request != 0 || b.RequestTitle != "" {
		t.Fatalf("loose item %+v", b)
	}
	path := "/api/left/" + itoa(b.ID) + "/request"
	if w := callAs(s, "POST", path, reportWorkerToken, reportWorkerRole, map[string]any{"request": rq.ID}); w.Code != 403 {
		t.Fatalf("worker attach: %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", path, chainCoordToken, chainCoordRole, map[string]any{"request": 999}); w.Code != 400 {
		t.Fatalf("unknown request: %d %s", w.Code, w.Body)
	}
	if w := callAs(s, "POST", path, chainCoordToken, chainCoordRole, map[string]any{"request": rq.ID}); w.Code != 200 {
		t.Fatalf("sender attach: %d %s", w.Code, w.Body)
	}
	if it := progressLeft(t, s)["정리"]; it.Request != rq.ID {
		t.Fatalf("after attach %+v", it)
	}
	if got := openKinds(bossRequest(t, s, rq.ID)); !slices.Contains(got, "left:"+itoa(b.ID)) {
		t.Fatalf("request remaining %v", got)
	}
	if w := callAs(s, "POST", path, reportBossToken, reportBossRole, map[string]any{"request": other.ID}); w.Code != 200 {
		t.Fatalf("boss attach: %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "POST", "/api/admin/left/"+itoa(b.ID)+"/request", `{"request":`+itoa(rq.ID)+`}`); w.Code != 200 {
		t.Fatalf("admin attach: %d %s", w.Code, w.Body)
	}
	if w := adminWrite(s, "POST", "/api/admin/left/"+itoa(b.ID)+"/request", `{"request":999}`); w.Code != 400 {
		t.Fatalf("admin unknown: %d %s", w.Code, w.Body)
	}
	if it := progressLeft(t, s)["정리"]; it.Request != rq.ID {
		t.Fatalf("after admin attach %+v", it)
	}
}

// TestRequestDoneRefusedWhileWorkLeft: marking a request done is refused
// while work is left under it, with each piece and the command that ends it
// in one answer; once all ended it closes. A request already done stays
// done when work turns up under it later (#564).
func TestRequestDoneRefusedWhileWorkLeft(t *testing.T) {
	t.Parallel()
	s := chainServer(t)
	rq := addRequest(t, s, "이름 바꾸기", "옛 이름 다 바꿔")
	done := func(want int) string {
		t.Helper()
		w := callAs(s, "POST", "/api/requests/"+itoa(rq.ID)+"/done", reportBossToken, reportBossRole, map[string]any{"result": "다 됨"})
		if w.Code != want {
			t.Fatalf("done: want %d, got %d %s", want, w.Code, w.Body)
		}
		return w.Body.String()
	}
	toCoord := chainSend(t, s, reportBossToken, reportBossRole, map[string]any{"recipient": chainCoordRole, "subject": "이름", "body": "x", "task_id": "t-1", "request_id": rq.ID})
	toWorker := chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "착수", "body": "x", "task_id": "t-1"})
	chainReply(t, s, reportWorkerToken, reportWorkerRole, toWorker, map[string]any{"body": "끝", "done": true, "left": []string{"문서"}})
	var leftID int64
	if err := s.db.QueryRow(`SELECT id FROM left_items WHERE directive_id=? AND kind='item'`, toWorker).Scan(&leftID); err != nil {
		t.Fatal(err)
	}
	msg := done(409)
	for _, want := range []string{"request " + itoa(rq.ID) + " has 2 left", "mail #" + itoa(toCoord) + " 이름 — complete " + itoa(toCoord) + " --left-none", "left #" + itoa(leftID) + " 문서 — left done " + itoa(leftID)} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal lacks %q:\n%s", want, msg)
		}
	}
	if d := bossRequest(t, s, rq.ID); d.Status != wire.RequestProgress || d.DoneAt != nil {
		t.Fatalf("refused done changed it: %+v", d.Request)
	}
	// A kept item (still open) blocks as well; passing it on is one way out.
	again := chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "문서 마저", "body": "x"})
	if w := callAs(s, "POST", "/api/left/"+itoa(leftID)+"/pass", chainCoordToken, chainCoordRole, map[string]any{"directive": again}); w.Code != 200 {
		t.Fatalf("pass: %d %s", w.Code, w.Body)
	}
	chainReply(t, s, chainCoordToken, chainCoordRole, toCoord, map[string]any{"body": "끝", "done": true, "left_none": true})
	if d := decodeRequest(t, callAs(s, "POST", "/api/requests/"+itoa(rq.ID)+"/done", reportBossToken, reportBossRole, map[string]any{"result": "다 됨"}), 200); d.Status != wire.RequestDone {
		t.Fatalf("done: %+v", d.Request)
	}
	// Work turning up under a done request leaves it done, and done again
	// (a new result) is not refused.
	late := chainSend(t, s, chainCoordToken, chainCoordRole, map[string]any{"recipient": reportWorkerRole, "subject": "하나 더", "body": "x", "task_id": "t-1"})
	d := bossRequest(t, s, rq.ID)
	if d.Status != wire.RequestDone || d.Remaining != 1 || d.RemainingList[0].ID != itoa(late) {
		t.Fatalf("late work: %+v %+v", d.Request, d.RemainingList)
	}
	done(200)
}
