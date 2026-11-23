package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// A collapsing alert reaches the event stream once, however often it comes
// inside its window, and the inbox and read show ×N and the last time (#525).
func TestAlertMailCollapses(t *testing.T) {
	t.Parallel()
	s, base := eventServer(t)
	st := openEvents(t, base, "secret-b", "b", nil, "")
	st.untilReady(t)
	a := mail.Alert{To: "b", Source: "watch:api", Subject: "api down", Body: "status 503", Urgent: true}
	first, err := s.mail().SendAlert(context.Background(), a)
	if err != nil || !first.New {
		t.Fatal(first, err)
	}
	ev := st.nextMessage(t)
	if int64(ev["id"].(float64)) != first.ID || ev["priority"] != "urgent" || ev["body"] != "status 503" || ev["sender"] != "switchboard/system" {
		t.Fatalf("event %v", ev)
	}
	for i := 0; i < 2; i++ {
		if r, err := s.mail().SendAlert(context.Background(), a); err != nil || r.New || r.ID != first.ID {
			t.Fatal(r, err)
		}
	}
	// The next event is the next mail, not the alert again.
	next := sendMail(t, s, "secret-a", "b", "after")
	if ev = st.nextMessage(t); int64(ev["id"].(float64)) != next {
		t.Fatalf("event after repeats %v, want #%d", ev, next)
	}

	w := call(s, "GET", "/api/messages?box=inbox", "secret-b", nil, "")
	var inbox []wire.Message
	if err := json.Unmarshal(w.Body.Bytes(), &inbox); err != nil {
		t.Fatal(w.Body.String())
	}
	var got *wire.Message
	for i := range inbox {
		if inbox[i].ID == first.ID {
			got = &inbox[i]
		}
	}
	if got == nil || got.AlertCount != 3 || got.AlertSource != "watch:api" || got.AlertLastAt == nil || got.Status != "pending" {
		t.Fatalf("inbox %s", w.Body)
	}
	w = call(s, "GET", "/api/messages/"+itoa(first.ID), "secret-b", nil, "")
	if !strings.Contains(w.Body.String(), `"alert_count":3`) {
		t.Fatalf("read %d %s", w.Code, w.Body)
	}
	if strings.Contains(sendBody(t, s, next), "alert_count") {
		t.Fatal("plain mail carries alert_count")
	}
}

func sendBody(t *testing.T, s *Server, id int64) string {
	t.Helper()
	return call(s, "GET", "/api/messages/"+itoa(id), "secret-b", nil, "").Body.String()
}
