package jobs

// The ledger's read model (#404). Three paths read the jobs tab: the
// dashboard's admin (/api/admin/jobs, /api/admin/schedules), a boss role's
// token (/api/boss/jobs, and the admin paths through the boss write guard)
// and a role's own token (/api/jobs, /api/schedules). Each builds a View —
// who is asking and which owners' rows it sees — and the rules that follow
// from it live here only: which rows a list shows, and that a list carries
// no command and no prompt. Server and dashboard code pass a View in; they
// do not filter rows or blank fields themselves (TestLedgerRulesStayInJobs).

import (
	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// View is who reads or changes the ledger and which owners' rows it sees.
type View struct {
	// Admin is the signed-in admin's name; "" for a role.
	Admin string
	// Role is the role whose token made the call; "" for an admin.
	Role string
	// scope reports whether a role's rows are in view; nil sees every role.
	scope func(role string) bool
}

// AdminView is the dashboard admin's view: every role's rows.
func AdminView(user string) View { return View{Admin: user} }

// ViewOf is a role's view: its own rows and, for a boss role, the rows of
// the roles in its scope (identity.BossScope; every role for a global boss).
func (s *Service) ViewOf(p identity.Principal) View {
	own := p.Role
	see, boss := s.ids.BossScope(p)
	return View{Role: own, scope: func(role string) bool { return role == own || boss && see(role) }}
}

// Sees reports whether role's rows are in view.
func (v View) Sees(role string) bool { return v.scope == nil || v.scope(role) }

// Ledger is the jobs tab: the jobs and schedules in a view, without
// commands or prompts.
type Ledger struct {
	Jobs      []wire.Job      `json:"jobs"`
	Schedules []wire.Schedule `json:"schedules"`
}

// Ledger lists the jobs and schedules in view, in state when given
// (StateLive: not ended), newest first.
func (s *Service) Ledger(v View, state string) (Ledger, error) {
	js, err := s.ListJobs(v, "", state)
	if err != nil {
		return Ledger{}, err
	}
	ss, err := s.ListSchedules(v, "", state)
	if err != nil {
		return Ledger{}, err
	}
	return Ledger{Jobs: js, Schedules: ss}, nil
}

// ListJobs lists the jobs in view, of role and in state when given, newest
// first (at most 500 before the view is applied); the command is left out.
func (s *Service) ListJobs(v View, role, state string) ([]wire.Job, error) {
	all, err := s.query(role, state)
	if err != nil {
		return nil, err
	}
	out := []wire.Job{}
	for _, j := range all {
		if v.Sees(j.Role) {
			j.Command = ""
			out = append(out, j)
		}
	}
	return out, nil
}

// ListSchedules lists the schedules in view, of role and in state when
// given, newest first (at most 500 before the view is applied); the prompt
// is left out.
func (s *Service) ListSchedules(v View, role, state string) ([]wire.Schedule, error) {
	all, err := s.schedules(role, state)
	if err != nil {
		return nil, err
	}
	out := []wire.Schedule{}
	for _, sc := range all {
		if v.Sees(sc.Role) {
			sc.Prompt = ""
			out = append(out, sc)
		}
	}
	return out, nil
}
