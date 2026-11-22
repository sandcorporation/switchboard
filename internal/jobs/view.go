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
	"errors"
	"net/http"

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

// OwnJobs is a role's list on its own token (/api/jobs): the jobs in view,
// of role and in state when given. The role's own rows carry the command;
// every other row (a boss role's list of another role) leaves it out, as
// every list does: another role's command is read one job at a time
// (ShowJob), and that read is recorded.
func (s *Service) OwnJobs(v View, role, state string) ([]wire.Job, error) {
	all, err := s.query(role, state)
	if err != nil {
		return nil, err
	}
	out := []wire.Job{}
	for _, j := range all {
		if v.Sees(j.Role) {
			if j.Role != v.Role {
				j.Command = ""
			}
			out = append(out, j)
		}
	}
	return out, nil
}

// OwnSchedules is OwnJobs for schedules: the prompt only on the role's own.
func (s *Service) OwnSchedules(v View, role, state string) ([]wire.Schedule, error) {
	all, err := s.schedules(role, state)
	if err != nil {
		return nil, err
	}
	out := []wire.Schedule{}
	for _, sc := range all {
		if v.Sees(sc.Role) {
			if sc.Role != v.Role {
				sc.Prompt = ""
			}
			out = append(out, sc)
		}
	}
	return out, nil
}

// ShowJob is one job with its command, cwd and machine, when it is in view
// (errNotFound otherwise, as for a missing one). A read by anyone but the
// owner is recorded (ledger_views): who, which job, whose, when; never the
// command.
func (s *Service) ShowJob(v View, id int64) (wire.Job, error) {
	j, err := s.Get(id)
	if err != nil {
		return j, err
	}
	if !v.Sees(j.Role) {
		return wire.Job{}, errNotFound
	}
	return j, s.recordView(v, "job", id, j.Role)
}

// ShowSchedule is one schedule with its condition and prompt, when it is in
// view; recorded as ShowJob is.
func (s *Service) ShowSchedule(v View, id int64) (wire.Schedule, error) {
	sc, err := s.GetSchedule(id)
	if err != nil {
		return sc, err
	}
	if !v.Sees(sc.Role) {
		return wire.Schedule{}, errNotFound
	}
	return sc, s.recordView(v, "schedule", id, sc.Role)
}

// recordView appends a read of one ledger item by v unless v is its owner.
func (s *Service) recordView(v View, kind string, id int64, owner string) error {
	if v.Admin == "" && v.Role == owner {
		return nil
	}
	viewer, admin := v.Role, 0
	if v.Admin != "" {
		viewer, admin = v.Admin, 1
	}
	_, err := s.db.Exec(`INSERT INTO ledger_views(viewer,admin,kind,item_id,owner,viewed_at) VALUES(?,?,?,?,?,?)`,
		viewer, admin, kind, id, owner, s.now().UnixMilli())
	return err
}

// writeShown writes a ShowJob or ShowSchedule result: 404 for an item not
// in view or missing, 500 when the read could not be recorded.
func writeShown[T wire.Job | wire.Schedule](w http.ResponseWriter, what string, v T, err error) {
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, "unknown "+what, http.StatusNotFound)
	case err != nil:
		http.Error(w, "query failed", http.StatusInternalServerError)
	default:
		writeJSON(w, http.StatusOK, v)
	}
}
