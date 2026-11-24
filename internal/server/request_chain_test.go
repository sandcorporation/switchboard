package server

import (
	"encoding/json"
	"slices"
	"testing"
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
