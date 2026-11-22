package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/asks"
	"github.com/sandcorporation/switchboard/internal/wire"
)

func askRelayAs(t *testing.T, s *Server, token, role string, relay any, want int) (wire.QuestionView, string) {
	t.Helper()
	w := callAs(s, "POST", "/api/questions", token, role, map[string]any{"questions": sampleQuestions(), "relay_to": relay})
	if w.Code != want {
		t.Fatalf("ask as %s relay %v: %d %s", role, relay, w.Code, w.Body)
	}
	if want != 201 {
		return wire.QuestionView{}, w.Body.String()
	}
	return decodeQuestion(t, w.Body.Bytes()), ""
}

func relayMail(t *testing.T, s *Server, id string) map[string][]mailRow {
	t.Helper()
	out := map[string][]mailRow{}
	for _, m := range questionMail(t, s, id) {
		if strings.HasPrefix(m.subject, "[질문 답 원문] ") {
			out[m.to] = append(out[m.to], m)
		}
	}
	return out
}

func TestQuestionRelayToMailsTheAnswerText(t *testing.T) {
	t.Parallel()
	s := questionServer(t)
	v, _ := askRelayAs(t, s, workerToken, "team/worker", []string{"a", "b", "a", " b "}, 201)
	if strings.Join(v.RelayTo, ",") != "a,b" {
		t.Fatalf("relay_to in view: %v", v.RelayTo)
	}
	if got, err := s.asks().AnswerQuestion(t.Context(), v.ID, asks.QuestionByDashboard, sampleAnswers()); err != nil || strings.Join(got.RelayTo, ",") != "a,b" {
		t.Fatalf("dashboard answer: %+v %v", got, err)
	}
	// A second answer is refused as before and mails nobody again.
	if _, err := s.asks().AnswerQuestion(t.Context(), v.ID, asks.QuestionByDashboard, sampleAnswers()); !errors.Is(err, asks.ErrQuestionNotPending) {
		t.Fatalf("second answer: %v", err)
	}
	answerQuestionAs(t, s, bossToken, "sandcorp/boss", v.ID, sampleAnswers(), 409)
	got := relayMail(t, s, v.ID)
	if len(got) != 2 || len(got["a"]) != 1 || len(got["b"]) != 1 {
		t.Fatalf("relay mail: %+v", got)
	}
	text := "배포: 지금 배포할까요? = 예\n대상: 어디에? = web, api / 기타: 천천히\n답한 쪽: dashboard"
	for _, box := range []string{"a", "b"} {
		m := got[box][0]
		if m.subject != "[질문 답 원문] "+v.ID+" team/worker 배포" || m.priority != "normal" || m.task != v.ID ||
			!strings.HasPrefix(m.body, "질문: "+v.ID+"\n묻는 역할: team/worker\n답한 곳: dashboard\n답한 시각: ") ||
			!strings.HasSuffix(m.body, "\n\n"+text) {
			t.Fatalf("relay mail to %s: %+v", box, m)
		}
	}
	// The asker still gets its one answer mail.
	var asker int
	for _, m := range questionMail(t, s, v.ID) {
		if m.to == "team/worker" {
			asker++
		}
	}
	if asker != 1 {
		t.Fatalf("asker mails: %d", asker)
	}
}

func TestQuestionRelayToSkipsWatchersAndAsker(t *testing.T) {
	t.Parallel()
	s := questionServer(t)
	if _, err := s.db.Exec(`UPDATE question_settings SET watchers='["a"]'`); err != nil {
		t.Fatal(err)
	}
	v, _ := askRelayAs(t, s, workerToken, "team/worker", []string{"a", "team/worker", "b"}, 201)
	answerQuestionAs(t, s, bossToken, "sandcorp/boss", v.ID, sampleAnswers(), 200)
	perBox := map[string]int{}
	for _, m := range questionMail(t, s, v.ID) {
		if strings.HasPrefix(m.subject, "[질문 답") {
			perBox[m.to]++
		}
	}
	if perBox["a"] != 1 || perBox["team/worker"] != 1 || perBox["b"] != 1 || len(perBox) != 3 {
		t.Fatalf("answer mails per box: %v", perBox)
	}
	if r := relayMail(t, s, v.ID); len(r) != 1 || len(r["b"]) != 1 || !strings.HasSuffix(r["b"][0].body, "답한 쪽: sandcorp/boss") {
		t.Fatalf("relay mail: %+v", r)
	}
}

func TestQuestionRelayToValidation(t *testing.T) {
	t.Parallel()
	s := groupServer(t)
	putGroup(t, s, "partner-a", "")
	cases := []struct {
		token, role string
		relay       any
		want        string
	}{
		{workerToken, "team/worker", []string{"a", "ghost"}, `unknown role "ghost"`},
		{workerToken, "team/worker", []string{""}, "empty address"},
		{workerToken, "team/worker", []string{"label:a/x"}, "unknown role"},
		{workerToken, "team/worker", []string{"a", "b", "team/worker", "team/worker-2", "a-2", "b-2", "a-3", "b-3", "a-4"}, "at most 8"},
		{partnerDevToken, partnerDevRole, []string{"b"}, "outside your group"},
	}
	for _, c := range cases {
		if _, body := askRelayAs(t, s, c.token, c.role, c.relay, 400); !strings.Contains(body, c.want) {
			t.Errorf("relay %v: %q, want %q", c.relay, body, c.want)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM questions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("stored %d %v", n, err)
	}
	// Eight distinct addresses (duplicates count once) are fine.
	v, _ := askRelayAs(t, s, workerToken, "team/worker", []string{"a", "b", "a", "team/worker-2", "a-2", "b-2", "a-3", "b-3", "a-4"}, 201)
	if len(v.RelayTo) != 8 {
		t.Fatalf("relay_to: %v", v.RelayTo)
	}
}

func TestQuestionWithoutRelayOmitsIt(t *testing.T) {
	t.Parallel()
	s := questionServer(t)
	w := callAs(s, "POST", "/api/questions", workerToken, "team/worker", map[string]any{"questions": sampleQuestions(), "relay_to": []string{}})
	if w.Code != 201 || strings.Contains(w.Body.String(), "relay_to") {
		t.Fatalf("ask: %d %s", w.Code, w.Body)
	}
	v := decodeQuestion(t, w.Body.Bytes())
	withdrawAs(t, s, workerToken, "team/worker", v.ID, 200)
	if m := questionMail(t, s, v.ID); len(m) != 0 {
		t.Fatalf("mail: %+v", m)
	}
	// Withdrawing a relayed question mails nobody either.
	r, _ := askRelayAs(t, s, workerToken, "team/worker", []string{"a"}, 201)
	withdrawAs(t, s, workerToken, "team/worker", r.ID, 200)
	if m := questionMail(t, s, r.ID); len(m) != 0 {
		t.Fatalf("withdraw mail: %+v", m)
	}
}
