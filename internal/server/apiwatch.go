package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/secrets"
	"github.com/sandcorporation/switchboard/internal/watch"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// watchInterval is how often the watcher looks for rules whose time has
// come; a rule's own period is at least a minute.
const watchInterval = 15 * time.Second

// watches is the API watch rules (#528) over this server's database and
// clock, with the role check, the secret store (#525, sealed under the
// server's secrets key) and the collapsing alert mail (#525). Tests may set
// s.watchHooks' fetch.
func (s *Server) watches() *watch.Service {
	h := s.watchHooks
	h.RoleExists = func(_ context.Context, role string) bool { return identity.RoleExists(s.db, role) }
	if h.Secrets == nil {
		h.Secrets = watchSecrets{s}
	}
	if h.Notify == nil {
		h.Notify = func(ctx context.Context, n watch.Notice) error {
			_, err := s.mail().SendAlert(ctx, mail.Alert{To: n.To, Source: n.Source, Subject: n.Subject, Body: n.Body, Urgent: n.Urgent, Window: n.Window})
			return err
		}
	}
	return watch.New(s.db, s.now, watch.DefaultLimits, h)
}

// watchSecrets is watch.Secrets over internal/secrets: a value is sealed
// under the server's key, opened only for the fetch.
type watchSecrets struct{ s *Server }

func (w watchSecrets) Put(_ context.Context, ref, value string) error {
	return w.s.secretsKey().Put(w.s.db, ref, value, w.s.now())
}

func (w watchSecrets) Get(_ context.Context, ref string) (string, error) {
	v, err := w.s.secretsKey().Open(w.s.db, ref)
	if errors.Is(err, secrets.ErrNotFound) || errors.Is(err, secrets.ErrUnreadable) {
		return "", watch.ErrSecretLost
	}
	return v, err
}

func (w watchSecrets) Delete(_ context.Context, ref string) error { return secrets.Delete(w.s.db, ref) }

func (w watchSecrets) Info(_ context.Context, ref string) (wire.SecretInfo, error) {
	return w.s.secretsKey().Info(w.s.db, ref)
}

// watchVisible is which rules a role's `watch list` shows: those that mail
// its base role or itself, every rule for a global boss.
func (s *Server) watchVisible(p identity.Principal) func(string) bool {
	all := s.ids().IsGlobalBoss(p)
	return func(to string) bool { return all || to == p.Base || to == p.Role }
}

// startWatcher runs the due watch rules every watchInterval until s.done
// closes, apart from the sweep: a fetch waits on another server and must
// not hold the sweep's notices back. The returned function waits for the
// goroutine to finish.
func (s *Server) startWatcher() func() {
	var wg sync.WaitGroup
	wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	goSafe("api watch", func() {
		defer wg.Done()
		t := time.NewTicker(watchInterval)
		defer t.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-t.C:
				s.runWatches(ctx)
			}
		}
	})
	go func() { <-s.done; cancel() }()
	return wg.Wait
}

// runWatches is one pass: the due rules run and the 설정 view redraws.
func (s *Server) runWatches(ctx context.Context) {
	n, err := s.watches().RunDue(ctx)
	if err != nil {
		log.Printf("api watch: %v", err)
	}
	if n > 0 {
		s.live.Publish("settings", "mail", "progress")
	}
}
