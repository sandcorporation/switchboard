package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/adminauth"
	"github.com/sandcorporation/switchboard/internal/asks"
	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/insight"
	"github.com/sandcorporation/switchboard/internal/jobs"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/presence"
	"github.com/sandcorporation/switchboard/internal/reports"
	"github.com/sandcorporation/switchboard/internal/requests"
	"github.com/sandcorporation/switchboard/internal/rules"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// queryer is the part of *sql.DB and *sql.Tx the server code reads through,
// so the same lookups work inside a transaction.
type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func (s *Server) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func ms(t time.Time) int64     { return t.UnixMilli() }
func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }

// inTx runs f in one transaction on the server's single connection.
func (s *Server) inTx(ctx context.Context, f func(*sql.Tx) error) error {
	return store.InTx(ctx, s.db, f)
}

// authenticate is the role check every /api/ request goes through: the bearer
// token against X-Switchboard-Role (or the old-prefix role header), by the
// rules in identity.Service.Authenticate.
func (s *Server) authenticate(r *http.Request) (identity.Principal, bool) {
	return s.ids().Authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), s.header(r, "Role"))
}

// ids is the identity rules over this server's database, clock, boss roles
// and group cache. A revoke logs the slot offline in the presence history
// and tells the senders of the role's pending mail (mail.NoticeRevoked).
func (s *Server) ids() *identity.Service {
	return identity.New(s.db, s.now, s.bossRoles, s.bossOrder, &s.groups, presence.LogOffline).
		WithRevokeNotice(func(tx *sql.Tx, role, stamp string, now time.Time) (func(), error) {
			boxes, err := mail.NoticeRevoked(tx, role, stamp, now)
			return func() {
				for _, box := range boxes {
					s.hub.Publish(box)
				}
			}, err
		})
}

// mail is the mailbox over this server's database, clock, settings and hub.
// An event stream re-checks its credentials with authenticate.
func (s *Server) mail() *mail.Service {
	return mail.New(s.db, s.now, &s.mailCfg, &s.hub, s.done, s.ids(), func(r *http.Request) bool {
		_, ok := s.authenticate(r)
		return ok
	}).WithFooter(s.ruleFooter)
}

// ruleFooter is the rules ledger's footer for a boss role's directive to
// base (#408 T3); none from other roles or with SWITCHBOARD_RULES_FOOTER=off.
// A ledger that cannot be read adds nothing rather than refusing the mail.
func (s *Server) ruleFooter(p identity.Principal, base string) string {
	if s.rulesFooterOff || !s.ids().IsBoss(p) {
		return ""
	}
	f, err := s.rules().Footer(base)
	if err != nil {
		log.Printf("rules footer: %v", err)
		return ""
	}
	return f
}

// presence is the slot and presence rules over this server's database,
// clock and settings. Held mail a slot change delivers wakes the hub.
func (s *Server) presence() *presence.Service {
	return presence.New(s.db, s.now, &s.presenceCfg, s.ids(), s.hub.Publish)
}

// asks is the question, decision and permission rules over this server's
// database, clock and settings. A new question whose settings ask for it is
// posted to Discord; an answer goes to the asking session's live slot
// (internal/presence).
func (s *Server) asks() *asks.Service {
	return asks.New(s.db, s.now, &s.asksCfg, s.ids(), s.mail(), &s.hub, s.done, s.discord.NoticeQuestion,
		asks.SlotRules{Asking: presence.AskingSlot, Answer: presence.AnswerSlot}).WithPersonAnswer(rules.RecordAnswer)
}

// insight is the metrics and alert rules over this server's database, clock,
// alert thresholds and boss roles. Alert mail wakes the hub.
func (s *Server) insight() *insight.Service {
	return insight.New(s.db, s.now, s.ids(), s.alerts, s.bossRoles, s.hub.Publish, s.aliasUse)
}

// reports is the boss reports over this server's database, clock and
// identity rules, with the base resolver the answered rule compares.
func (s *Server) reports() *reports.Service {
	return reports.New(s.db, s.now, s.ids(), insight.NewBaseResolver(s.db).Of)
}

// requests is the owner's requests (#501) over this server's database,
// clock and identity rules.
func (s *Server) requests() *requests.Service {
	return requests.New(s.db, s.now, s.ids(), s.mail().DirectiveStates)
}

// rules is the rules ledger over this server's database and clock, with
// identity's groups and asks' answered questions passed in (#406).
func (s *Server) rules() *rules.Service {
	ids := s.ids()
	return rules.New(s.db, s.now, rules.Hooks{
		GroupOf: func(role string) string { g, _ := ids.GroupOf(role); return g.Prefix },
		Answers: func(qs []string) ([]wire.Decision, error) { return s.asks().DecisionsByID(qs) },
		Notice:  s.noticeGlobalBosses,
	})
}

// noticeGlobalBosses mails each global boss role a notice that asks no
// reply, once per idem (a rule proposal waiting for approval, #409).
func (s *Server) noticeGlobalBosses(subject, body, idem string) {
	roles := make([]string, 0, len(s.bossRoles))
	for r := range s.bossRoles {
		roles = append(roles, r)
	}
	slices.Sort(roles)
	for _, role := range roles {
		if err := s.notifyRole(context.Background(), role, subject, body, idem+"-"+role); err != nil {
			log.Printf("notice %s: %v", role, err)
		}
	}
}

// ruleCaller is p as the rules ledger sees it: a global boss reads in full
// and writes; any other role reads its own group's rows that apply to it.
func (s *Server) ruleCaller(p identity.Principal) rules.Caller {
	ids := s.ids()
	base := p.Base
	if base == "" {
		base = p.Role
	}
	if ids.IsGlobalBoss(p) {
		return rules.Caller{Role: base, Full: true}
	}
	g, _ := ids.GroupOf(base)
	return rules.Caller{Role: base, Group: g.Prefix}
}

// jobs is the job ledger over this server's database, clock and identity
// rules. An end mail wakes the owner's waiters.
func (s *Server) jobs() *jobs.Service {
	return jobs.New(s.db, s.now, s.ids(), s.hub.Publish).WithNotify(s.notifyRole)
}

// progressAPI serves the progress view with the metrics alert limit and base
// resolver (internal/insight).
func (s *Server) progressAPI(w http.ResponseWriter, r *http.Request, allowed bool) {
	s.presence().ProgressAPI(w, r, allowed, s.insight().Limits().Pending, insight.NewBaseResolver(s.db).Of)
}

// progress is the progress report as of now.
func (s *Server) progress() (wire.ProgressReport, error) {
	return s.presence().Progress(s.insight().Limits().Pending, insight.NewBaseResolver(s.db).Of)
}

// migrateAnsweredRule runs presence.MigrateAnsweredRule at start-up and logs
// one line when it wrote the stored version.
func (s *Server) migrateAnsweredRule(logf func(string, ...any)) error {
	m, err := s.presence().MigrateAnsweredRule(insight.NewBaseResolver(s.db).Of)
	if err != nil || !m.Changed {
		return err
	}
	logf("%s", m.Line())
	return nil
}

// ledgerView is the job ledger's view of an admin path's caller (#404): the
// boss role whose token reached it, or the signed-in admin.
func (s *Server) ledgerView(who adminauth.Identity, boss *identity.Principal) jobs.View {
	if boss != nil {
		return s.jobs().ViewOf(*boss)
	}
	return jobs.AdminView(who.User)
}

// notifyRole stores a normal system mail to role's mailbox once per idem key
// and wakes its waiters: a notice that needs no answer (a system mail is
// never an open directive). The job ledger tells a schedule's owner of a
// pause or resume by someone else with it (#404).
func (s *Server) notifyRole(ctx context.Context, role, subject, body, idem string) error {
	var box string
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		box = ""
		to, ok := identity.Mailbox(tx, role)
		if !ok {
			return nil
		}
		stored, err := mail.InsertSystemMailOnce(tx, to, subject, body, "", "normal", idem, s.now())
		if stored {
			box = to
		}
		return err
	})
	if err == nil && box != "" {
		s.hub.Publish(box)
	}
	return err
}
