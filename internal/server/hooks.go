package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/sandcorporation/switchboard/internal/adminauth"
	"github.com/sandcorporation/switchboard/internal/hooks"
	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// hookState is the incoming webhooks' state that outlives a request (#527):
// the per-hook rate limit, the forward pass lock and the sending client.
func (s *Server) hookState() *hooks.State {
	s.hooksOnce.Do(func() {
		if s.hooksSt == nil {
			s.hooksSt = hooks.NewState()
		}
	})
	return s.hooksSt
}

// hooks is the incoming webhooks over this server's database and clock,
// with the secret store and the alert mail passed in.
func (s *Server) hooks() *hooks.Service {
	return hooks.New(s.db, s.now, s.hookState(), hooks.Hooks{
		Secrets:   s.hookSecrets(),
		Alert:     s.hookAlert,
		RoleKnown: func(role string) bool { return identity.RoleExists(s.db, role) },
		Kick:      s.kickForward,
	})
}

// hookSecrets is the secret store the hooks seal their destinations with.
func (s *Server) hookSecrets() hooks.Secrets { return nil }

// hookAlert sends one hook notice as alert mail.
func (s *Server) hookAlert(ctx context.Context, a hooks.Alert) error {
	return errors.New("alert mail is not available")
}

// kickForward sends on the receipts just stored, off the request.
func (s *Server) kickForward() {
	go func() {
		n, err := s.hooks().ForwardDue(context.Background())
		if err != nil {
			log.Printf("hooks: forward: %v", err)
		}
		if n > 0 {
			s.live.Publish("settings", "mail")
		}
	}()
}

// receiveHook serves POST /hook/<value>: no sign-in, the value is the key.
func (s *Server) receiveHook(w http.ResponseWriter, r *http.Request) {
	sw := &statusWriter{ResponseWriter: w}
	s.hooks().Receive(sw, r, strings.TrimPrefix(r.URL.Path, "/hook/"))
	if sw.code == http.StatusOK {
		s.live.Publish("settings", "mail")
	}
}

// hooksAdmin serves /api/admin/hooks…: reading for a login that may view,
// writing for a signed-in login allowed to manage sessions (#476); a boss
// role's writes do not reach it (boss_write.go has no row for it).
func (s *Server) hooksAdmin(w http.ResponseWriter, r *http.Request, rest string, who adminauth.Identity, boss *identity.Principal) {
	if r.Method == http.MethodGet {
		if boss == nil && !who.Can(wire.PermView) {
			refusePerm(w, wire.PermView)
			return
		}
	} else {
		if boss != nil {
			http.Error(w, "forbidden: only a dashboard login changes hooks", http.StatusForbidden)
			return
		}
		if !who.Can(wire.PermSessions) {
			refusePerm(w, wire.PermSessions)
			return
		}
	}
	s.hooks().AdminAPI(w, r, who.User, rest)
}
