package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/presence"
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
	writeJSON(w, adminSettings{Version: version, Hosting: s.hosting, Reminders: rs, Rule: rule, LeftRemind: lr})
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
	after, err := mail.ParseLeftRemind(in.After)
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
// to the "admin auth: …" line: "directive reminders: on (after 2h, max 3,
// since first sweep)" or "directive reminders: off".
func (s *Server) logRemindStart() error {
	rs, err := s.presence().RemindStatus(context.Background())
	if err != nil {
		return err
	}
	log.Print(rs.StartLine())
	return nil
}
