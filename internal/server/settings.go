package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/sandcorporation/switchboard/internal/insight"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/presence"
	"github.com/sandcorporation/switchboard/internal/remind"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// adminSettings is GET /api/admin/settings (#223): the server version and
// the settings an operator needs to confirm after a deploy, read-only. It
// holds no secret (no token, webhook address or client secret).
type adminSettings struct {
	Version   string                `json:"version"`
	Hosting   bool                  `json:"hosting"`
	Reminders presence.RemindStatus `json:"reminders"`
	Rule      presence.RuleStatus   `json:"answered_rule"`
	// LeftRemind is the reminder of open left items (#553).
	LeftRemind wire.LeftRemind `json:"left_remind"`
	// Remind is one row per reminder kind (#586).
	Remind []wire.RemindKind `json:"remind"`
}

func (s *Server) settingsAPI(w http.ResponseWriter, r *http.Request) {
	rs, err := s.presence().RemindStatus(r.Context())
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	rule, err := s.presence().RuleStatus()
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	lr, err := s.mail().LeftRemind(r.Context())
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	kinds, err := s.remindKinds(r.Context())
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, adminSettings{Version: version, Hosting: s.hosting, Reminders: rs, Rule: rule, LeftRemind: lr, Remind: kinds})
}

// remindTo is who each kind's reminders go to.
var remindTo = map[remind.Kind]string{
	remind.Mail:    "지시 받은 역할",
	remind.Left:    "지시한 역할",
	remind.Request: "맡은 대장",
}

// remindKinds is the reminder settings' rows (#586), in remind.Kinds order.
func (s *Server) remindKinds(ctx context.Context) ([]wire.RemindKind, error) {
	out := make([]wire.RemindKind, 0, len(remind.Kinds))
	for _, k := range remind.Kinds {
		set, err := remind.Load(ctx, s.db, k, s.now().UTC())
		if err != nil {
			return nil, err
		}
		since := set.Since
		v := wire.RemindKind{Kind: string(k), To: remindTo[k], On: set.On(), AfterSeconds: int64(set.After / time.Second), Since: &since}
		if set.On() {
			v.After = mail.ShortDuration(set.After)
		}
		switch k {
		case remind.Mail:
			v.Overdue, err = s.presence().MailOverdue(ctx, insight.NewBaseResolver(s.db).Of)
		case remind.Left:
			v.Overdue, err = s.mail().LeftOverdue(ctx)
		case remind.Request:
			v.Overdue, v.Waiting, err = s.requests().RemindCounts(ctx)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// remindAPI is PUT /api/admin/settings/remind/{kind} (#586): {"after":
// "2h"} sets that kind's reminder time, {"after": ""} turns it off; a new
// value counts from now. It answers the kind's row.
func (s *Server) remindAPI(w http.ResponseWriter, r *http.Request, kind string) {
	k := remind.Kind(kind)
	if !remind.Valid(k) {
		http.NotFound(w, r)
		return
	}
	var in wire.RemindSet
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	after, err := remind.Parse(in.After)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err = remind.Save(r.Context(), s.db, k, after, s.now().UTC()); err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	kinds, err := s.remindKinds(r.Context())
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	for _, v := range kinds {
		if v.Kind == kind {
			writeJSON(w, v)
		}
	}
}

// leftRemindAPI is PUT /api/admin/settings/left-remind (#553): {"after":
// "24h"} sets the reminder time of open left items, {"after": ""} turns it
// off. It answers the setting in force.
func (s *Server) leftRemindAPI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		After string `json:"after"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	after, err := remind.Parse(in.After)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	lr, err := s.mail().SetLeftRemind(r.Context(), after)
	if err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, lr)
}

// logRemindStart logs the one start-up line for directive reminders, next
// to the "admin auth: …" line: "directive reminders: on (after 2h, since
// <time>, idle 1h, idle max 1)" or "directive reminders: off (…)".
func (s *Server) logRemindStart() error {
	rs, err := s.presence().RemindStatus(context.Background())
	if err != nil {
		return err
	}
	log.Print(rs.StartLine())
	return nil
}
