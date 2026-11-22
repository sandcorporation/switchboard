package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/adminauth"
	"github.com/sandcorporation/switchboard/internal/asks"
	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// bossRoles is GET /api/boss/roles: roles (with their group and whether
// revoked), the leased numbered slots and the groups.
type bossRoles struct {
	Roles  []identity.RoleView `json:"roles"`
	Slots  []wire.SlotView     `json:"slots"`
	Groups []identity.Group    `json:"groups"`
}

// secretSetting names a setting that holds a secret and whether it is set;
// its value is never sent.
type secretSetting struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

// bossSettings is GET /api/boss/settings: the question settings, the recent
// refusals in view and, for a global boss, the server summary the
// dashboard's settings tab shows, the admin sign-in mode and the secret
// settings by name.
type bossSettings struct {
	QuestionSettings []asks.QuestionSettings `json:"question_settings"`
	QuestionRefusals []wire.QuestionRefusal  `json:"question_refusals"`
	Server           *adminSettings          `json:"server,omitempty"`
	AdminAuth        string                  `json:"admin_auth,omitempty"`
	Secrets          []secretSetting         `json:"secrets,omitempty"`
}

// bossAPI serves GET /api/boss/{view} (#397): the dashboard's read-only
// views for a boss role's token. A global boss sees everything; a group boss
// the roles of its groups (identity.BossScope). Other methods are the boss
// role's writes (bossWrite, #399).
//
//	messages        all mail, ?open=1 &from= &to= &since=RFC3339 &limit=N
//	messages/{id}   one message, state untouched, the view recorded
//	jobs            jobs and schedules, ?state=
//	roles           roles, slots and groups
//	settings        question settings, refusals, server summary
//	terminals       hosted session records in view
//	nodes           nodes that run a session in view (all for a global boss)
//	autos           scheduled runs of the sessions in view
func (s *Server) bossAPI(w http.ResponseWriter, r *http.Request, who identity.Principal, view string) {
	if r.Method != http.MethodGet || bossReadThrough(view) {
		s.bossWrite(w, r, who, view)
		return
	}
	ids := s.ids()
	see, ok := ids.BossScope(who)
	if !ok {
		http.Error(w, "boss roles only", http.StatusForbidden)
		return
	}
	global := ids.IsGlobalBoss(who)
	q := r.URL.Query()
	fail := func() { http.Error(w, "query failed", http.StatusInternalServerError) }
	if rest, ok := strings.CutPrefix(view, "messages/"); ok {
		id, err := wire.ParseID(rest)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		m, err := s.mail().BossShow(who, mail.CurrentID(s.db, id), see)
		switch {
		case errors.Is(err, mail.ErrNotInView):
			http.Error(w, "not found", http.StatusNotFound)
		case err != nil:
			fail()
		default:
			writeJSON(w, m)
		}
		return
	}
	switch view {
	case "messages":
		f := mail.BossFilter{Open: q.Get("open") == "1", From: q.Get("from"), To: q.Get("to")}
		if v := q.Get("since"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				http.Error(w, "since: an RFC 3339 time", http.StatusBadRequest)
				return
			}
			f.Since = t
		}
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 500 {
				http.Error(w, "limit: 1-500", http.StatusBadRequest)
				return
			}
			f.Limit = n
		}
		out, err := s.mail().BossList(f, see)
		if err != nil {
			fail()
			return
		}
		writeJSON(w, out)
	case "jobs":
		// The ledger's read model applies the view (jobs.View, #404).
		ledger := s.jobs()
		out, err := ledger.Ledger(ledger.ViewOf(who), q.Get("state"))
		if err != nil {
			fail()
			return
		}
		writeJSON(w, out)
	case "roles":
		roles, err := ids.ListRoles()
		if err != nil {
			fail()
			return
		}
		slots, err := s.presence().ActiveSlots("")
		if err != nil {
			fail()
			return
		}
		groups, err := ids.ListGroups()
		if err != nil {
			fail()
			return
		}
		out := bossRoles{Roles: []identity.RoleView{}, Slots: []wire.SlotView{}, Groups: []identity.Group{}}
		for _, v := range roles {
			if see(v.Role) {
				out.Roles = append(out.Roles, v)
			}
		}
		for _, v := range slots {
			if see(v.Base) {
				out.Slots = append(out.Slots, v)
			}
		}
		for _, g := range groups {
			if global || see(g.Prefix+"/"+groupProbe) {
				out.Groups = append(out.Groups, g)
			}
		}
		writeJSON(w, out)
	case "settings":
		out, err := s.bossSettings(r, see, global)
		if err != nil {
			fail()
			return
		}
		writeJSON(w, out)
	case "terminals", "nodes", "autos":
		if !s.hosting {
			http.Error(w, "terminal hosting is off on this server", http.StatusNotFound)
			return
		}
		s.bossHosting(w, view, see, global)
	default:
		http.NotFound(w, r)
	}
}

// groupProbe completes a group prefix (or a question target's PREFIX/*) to a
// role name under it, to ask the scope about the group itself.
const groupProbe = "x"

// bossSettings builds GET /api/boss/settings for a view see.
func (s *Server) bossSettings(r *http.Request, see func(string) bool, global bool) (bossSettings, error) {
	out := bossSettings{QuestionSettings: []asks.QuestionSettings{}, QuestionRefusals: []wire.QuestionRefusal{}}
	qs, err := s.asks().QuestionSettingsList()
	if err != nil {
		return out, err
	}
	for _, v := range qs {
		target := v.Target
		if p, ok := strings.CutSuffix(target, "/*"); ok {
			target = p + "/" + groupProbe
		}
		if global || target == store.DefaultQuestionTarget || see(target) {
			out.QuestionSettings = append(out.QuestionSettings, v)
		}
	}
	refusals, err := s.asks().QuestionRefusals()
	if err != nil {
		return out, err
	}
	for _, v := range refusals {
		if see(v.Role) {
			out.QuestionRefusals = append(out.QuestionRefusals, v)
		}
	}
	if !global {
		return out, nil
	}
	rs, err := s.presence().RemindStatus(r.Context())
	if err != nil {
		return out, err
	}
	rule, err := s.presence().RuleStatus()
	if err != nil {
		return out, err
	}
	out.Server = &adminSettings{Version: version, Hosting: s.hosting, Reminders: rs, Rule: rule}
	out.AdminAuth = adminauth.StartLine(&s.admin)
	p := config.EnvPrefix
	out.Secrets = []secretSetting{
		{p + "ADMIN_PROXY_TOKEN", s.admin.ProxyToken != ""},
		{p + "OIDC_CLIENT_SECRET", s.admin.OIDCClientSecretSet()},
		{p + "DISCORD_WEBHOOK", s.discord != nil},
	}
	return out, nil
}

// bossHosting writes the terminal tab's lists for a view see: records whose
// base is in view (as the dashboard shows them; env names that look like
// secrets are refused when a record is added), their scheduled runs, and the nodes that run them (every node for
// a global boss).
func (s *Server) bossHosting(w http.ResponseWriter, view string, see func(string) bool, global bool) {
	recs, err := s.hostingHub().List()
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	inView := map[string]bool{}
	nodes := map[string]bool{}
	shown := []wire.HostedSession{}
	for _, rec := range recs {
		if !see(rec.Base) {
			continue
		}
		inView[rec.ID], nodes[rec.Node] = true, true
		shown = append(shown, rec)
	}
	switch view {
	case "terminals":
		writeJSON(w, shown)
	case "nodes":
		all, err := s.hostingService().Nodes()
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		out := []wire.NodeView{}
		for _, n := range all {
			if global || nodes[n.Name] {
				out = append(out, n)
			}
		}
		writeJSON(w, out)
	case "autos":
		all, err := s.hostingService().Autos("")
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		out := []wire.HostAuto{}
		for _, a := range all {
			if inView[a.Session] {
				out = append(out, a)
			}
		}
		writeJSON(w, out)
	}
}
