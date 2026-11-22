package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/adminauth"
	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/dashboard"
	"github.com/sandcorporation/switchboard/internal/hosting"
	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/mail"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Terminal hosting (#173) is compiled in but off. SWITCHBOARD_HOSTING=on turns
// it on; the operations files may name it but never carry an address
// (internal/config guard test). While off, no hosting route, table, tab or
// live kind is observable.

const (
	defaultListenAddr = ":8080"
	containerDBPath   = "/data/mail.db" // the Dockerfile's DB_PATH
)

// hostingStart reads SWITCHBOARD_HOSTING and, when it is on, refuses an
// unsafe start: a remote listener must pass hostingRemote, and the browser
// terminal settings must pass terminalStart (which also asks for admin
// sign-in when the terminal is not loopback-only). The error names every
// failing condition.
func hostingStart(getenv func(string) string) (bool, error) {
	on, from := config.HostingFrom(getenv)
	if !on {
		return false, nil
	}
	var failed []string
	if _, err := hostingRemote(getenv); err != nil {
		failed = append(failed, err.Error())
	}
	if err := terminalStart(getenv); err != nil {
		failed = append(failed, err.Error())
	}
	if len(failed) > 0 {
		return true, errors.New(from + "=on refused: " + strings.Join(failed, "; "))
	}
	return true, nil
}

// loopbackAddr reports whether a listen address binds only loopback. An empty
// host (":8080") binds every interface and is not loopback.
func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostingAdminPath reports whether an /api/admin/ path (prefix removed) is a
// hosting route: terminals…, nodes…, host-grants, node-invites (#446) or
// admin-actions (#446).
func hostingAdminPath(path string) bool {
	for _, p := range []string{"terminals", "nodes", "host-grants", "node-invites", "admin-actions"} {
		if rest, ok := strings.CutPrefix(path, p); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
			return true
		}
	}
	return false
}

// hostingRolePath reports whether an /api/ path (prefix removed) is the
// role hosting route (#369): host/….
func hostingRolePath(path string) bool { return strings.HasPrefix(path, "host/") }

// hostingNodePath reports whether an /api/ path (prefix removed) is the node
// channel: node/….
func hostingNodePath(path string) bool { return strings.HasPrefix(path, "node/") }

// hostingStub answers a hosting route while hosting is on; later tickets
// replace it with the real handlers.
func hostingStub(w http.ResponseWriter) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// hostingAdmin serves an /api/admin/ hosting path (prefix removed) for an
// identified admin past the write guard: the records and the node list, and
// the stub for the routes of later tickets. who (and boss, for a boss
// role's write) goes on the request as the actor of the admin actions the
// handlers record (#446).
func (s *Server) hostingAdmin(w http.ResponseWriter, r *http.Request, path string, who adminauth.Identity, boss *identity.Principal) {
	method := who.Mode
	if boss != nil {
		method = "boss"
	}
	r = hosting.WithActor(r, hosting.Actor{Admin: who.User, Method: method, Addr: s.adminAuth().ClientIP(r)})
	if terminalPath(path) {
		switch code := s.terminalGate(r); code {
		case http.StatusNotFound:
			http.NotFound(w, r)
			return
		case http.StatusForbidden:
			http.Error(w, "forbidden: client address outside "+config.EnvPrefix+config.HostingTerminalAllowName, http.StatusForbidden)
			return
		}
	}
	switch path {
	case "host-grants":
		s.hostingService().AdminGrantsAPI(w, r)
		return
	case "node-invites":
		s.hostingService().AdminInviteAPI(w, r)
		return
	case "admin-actions":
		s.hostingService().AdminActionsAPI(w, r)
		return
	}
	if rest, ok := strings.CutPrefix(path, "terminals"); ok && s.hostingHub().AdminTerminalsAPI(w, r, strings.TrimPrefix(rest, "/")) {
		return
	}
	if rest, ok := strings.CutPrefix(path, "nodes"); ok && s.hostingHub().AdminNodesAPI(w, r, strings.TrimPrefix(rest, "/")) {
		return
	}
	hostingStub(w)
}

// assets serves the dashboard's vendored files (the terminal view's
// xterm.js) while hosting is on; with it off they are an unknown path.
func (s *Server) assets(w http.ResponseWriter, r *http.Request) {
	if !s.hosting {
		s.root(w, r)
		return
	}
	dashboard.Assets(w, r)
}

// hostingHub is the node channel over this server's database; its changes
// publish the live kind "terminals".
func (s *Server) hostingHub() *hosting.Hub {
	s.hubOnce.Do(func() {
		s.nodeHub = hosting.NewHub(s.hostingService(), s.live.Publish)
		a := s.termLimits
		a.Session = func(r *http.Request) (string, string, bool) {
			who, key, ok := s.adminAuth().Session(r)
			return who.User, key, ok
		}
		a.Origin = s.liveOriginAllowed
		a.ClientIP = func(r *http.Request) string { return s.adminAuth().ClientIP(r) }
		s.nodeHub.SetAdmin(a)
		bosses := make([]string, 0, len(s.bossRoles))
		for r := range s.bossRoles {
			bosses = append(bosses, r)
		}
		sort.Strings(bosses)
		s.nodeHub.SetNotify(s.hostingNotify, bosses)
		s.nodeHub.SetAutoMail(s.hostingAutoMail)
		s.nodeHub.SetAutoMailDone(func(id int64) (bool, error) { return mail.Completed(s.db, mail.CurrentID(s.db, id)) })
		s.nodeHub.SetAutoAlert(s.hostingAutoAlert)
		s.nodeHub.SetSlotOf(s.hostingSlotOf)
	})
	return s.nodeHub
}

// hostingSlotOf is the hub's SlotOf, read from presence's slots: every slot
// of base at machine in directory or its session-marked form. Seen is the
// last register or heartbeat (the lease end minus one lease).
func (s *Server) hostingSlotOf(base, machine, directory string) ([]hosting.Slot, error) {
	p := s.presence()
	vs, err := p.FolderSlots(base, machine, directory)
	if err != nil {
		return nil, err
	}
	out := make([]hosting.Slot, 0, len(vs))
	for _, v := range vs {
		out = append(out, hosting.Slot{Role: v.Role, Directory: v.Directory, SessionID: v.SessionID, LeaseUntil: v.LeaseExpiresAt,
			Seen: v.LeaseExpiresAt.Add(-p.LeaseLength()), Live: v.LeaseExpiresAt.After(s.now())})
	}
	return out, nil
}

// hostingNotify is the hub's Notify: one system mail per role's mailbox in
// one transaction, idempotent by key (suffixed with the mailbox when there
// are several, since keys are unique per sender). A role without a mailbox
// is skipped.
func (s *Server) hostingNotify(to []string, subject, body, key string) error {
	var woke []string
	err := store.InTx(context.Background(), s.db, func(tx *sql.Tx) error {
		woke = woke[:0]
		seen := map[string]bool{}
		for _, role := range to {
			box, ok := identity.Mailbox(tx, role)
			if !ok || seen[box] {
				if !ok {
					log.Printf("hosting notice %s skipped for %q: not a registered role", key, role)
				}
				continue
			}
			seen[box] = true
			idem := key
			if len(to) > 1 {
				idem = key + ":" + box
			}
			if err := mail.InsertSystemMail(tx, box, subject, body, "", "normal", idem, s.now()); err != nil {
				return err
			}
			woke = append(woke, box)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, box := range woke {
		s.hub.Publish(box)
	}
	return nil
}

// hostingAutoAlert is the hub's AutoAlert (#336): a Discord notice, and a
// system mail to each of the scheduled run's notify roles, idempotent by
// the run.
func (s *Server) hostingAutoAlert(n hosting.AutoNotice) {
	a := n.Auto
	was := ""
	if n.Resolved {
		was = n.Was
	}
	s.discord.NoticeScheduledRun(a.ID, a.Session, n.Role, n.Run.Result, was)
	if len(a.Notify) == 0 {
		return
	}
	name := a.Label
	if name == "" {
		name = fmt.Sprintf("scheduled run %d", a.ID)
	}
	at := time.UnixMilli(n.Run.At).UTC().Format(time.RFC3339)
	var subject, body string
	if n.Resolved {
		subject = fmt.Sprintf("scheduled run resolved: %s (was %s)", name, n.Was)
		body = fmt.Sprintf("Scheduled run %d of hosted session %s (%s) ran %s at %s, after %s.", a.ID, a.Session, n.Role, n.Run.Result, at, n.Was)
	} else {
		subject = fmt.Sprintf("scheduled run %s: %s", n.Run.Result, name)
		body = fmt.Sprintf("Scheduled run %d of hosted session %s (%s) ended %s at %s (run %d).", a.ID, a.Session, n.Role, n.Run.Result, at, n.Run.ID)
		if n.Run.Detail != "" {
			body += "\n\n" + n.Run.Detail
		}
		if n.Run.Output != "" {
			body += "\n\nPrecheck output (end):\n" + n.Run.Output
		}
		body += "\n\nFurther runs with the same result are not mailed; a fired or skipped run sends \"resolved\"."
	}
	key := fmt.Sprintf("hostauto-alert:%d:%d", a.ID, n.Run.ID)
	if err := s.hostingNotify(a.Notify, subject, body, key); err != nil {
		log.Printf("hosting: scheduled run %d alert mail: %v", a.ID, err)
	}
}

// hostingAutoMail is the hub's AutoMail: one system mail, idempotent by
// key, and the mail's id (the first one's on a repeat). Mail for a record's
// session (#364) goes to its live slot's mailbox or is held for it
// (mail.InsertSessionMail); without a machine it goes to the base's mailbox.
func (s *Server) hostingAutoMail(to hosting.AutoMailTo, subject, body, key string) (int64, error) {
	var id int64
	var box string
	err := store.InTx(context.Background(), s.db, func(tx *sql.Tx) error {
		var ok bool
		if to.Machine != "" {
			if !identity.RoleExists(tx, to.Base) {
				return fmt.Errorf("%q is not a registered role", to.Base)
			}
			box = ""
			if to.Slot != "" {
				if box, ok = identity.Mailbox(tx, to.Slot); !ok {
					return fmt.Errorf("%q is not a registered role", to.Slot)
				}
			}
			var err error
			id, _, err = mail.InsertSessionMail(tx, box, mail.Handoff{Base: to.Base, Machine: to.Machine, Directory: to.Directory}, subject, body, key, s.now())
			return err
		}
		if box, ok = identity.Mailbox(tx, to.Base); !ok {
			return fmt.Errorf("%q is not a registered role", to.Base)
		}
		if err := mail.InsertSystemMail(tx, box, subject, body, "", "normal", key, s.now()); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT id FROM messages WHERE sender=? AND idem=?`, identity.SystemSender, key).Scan(&id)
	})
	if err != nil {
		return 0, err
	}
	if box != "" {
		s.hub.Publish(box)
	}
	return id, nil
}

// hostingService is the hosted-session store over this server's database.
func (s *Server) hostingService() *hosting.Service {
	return hosting.New(s.db, s.now, func(base string) bool { return identity.RoleExists(s.db, base) }).WithCredentials(roleCredentials(s.db, s.now))
}

// roleCredentials is identity's role credential store as hosting takes it
// (#343).
func roleCredentials(db *sql.DB, clock func() time.Time) *hosting.RoleCredentials {
	c := identity.NewCredentials(db, clock)
	return &hosting.RoleCredentials{
		Issue: c.Issue,
		Live: func() ([]hosting.LiveCredential, error) {
			live, err := c.Live()
			out := make([]hosting.LiveCredential, 0, len(live))
			for _, v := range live {
				out = append(out, hosting.LiveCredential{ID: v.ID, Record: v.Record, Node: v.Node})
			}
			return out, err
		},
		Revoke: c.Revoke,
	}
}

// HostCLI is the binary's `host` command: the hosted-session records in
// DB_PATH, which must name this instance's own database (the same rule as
// SWITCHBOARD_HOSTING=on), so the operations database never gains records.
func HostCLI(action, id string, add wire.HostedSessionCreate, jsonOut bool, stdout io.Writer) (int, error) {
	return withInstanceDB("host", func(svc *hosting.Service) error { return svc.RunCLI(action, id, add, jsonOut, stdout) })
}

// HostGrantCLI is the binary's `host grant` and `host revoke-grant` (#369):
// the role grants in DB_PATH (the same rule as `host`).
func HostGrantCLI(action string, g wire.HostGrant, jsonOut bool, stdout io.Writer) (int, error) {
	return withInstanceDB("host "+action, func(svc *hosting.Service) error { return svc.RunGrantCLI(action, g, jsonOut, stdout) })
}

// HostGroupCLI is the binary's `host group` command: the work groups in
// DB_PATH (the same rule as `host`).
func HostGroupCLI(action, name string, threshold *int, jsonOut bool, stdout io.Writer) (int, error) {
	return withInstanceDB("host group "+action, func(svc *hosting.Service) error {
		return svc.RunGroupCLI(action, name, threshold, jsonOut, stdout)
	})
}

// HostAutoCLI is the binary's `host auto` command: the scheduled runs of
// the hosted sessions in DB_PATH (the same rule as `host`).
func HostAutoCLI(action, ref string, c wire.HostAutoCreate, runs int, jsonOut bool, stdout io.Writer) (int, error) {
	return withInstanceDB("host auto", func(svc *hosting.Service) error { return svc.RunAutoCLI(action, ref, c, runs, jsonOut, stdout) })
}

// NodeInviteCLI is the binary's `node invite NAME`: a one-time code for a new
// node, valid 15 minutes, against DB_PATH (the same rule as `host`). The code
// is printed once; the database keeps only its hash.
func NodeInviteCLI(name string, stdout io.Writer) (int, error) {
	return withInstanceDB("node invite", func(svc *hosting.Service) error {
		code, expires, err := svc.InviteNode(name)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "%s\nnode %s: use once before %s with\n  switchboard node --host URL --join %s\n", code, name, expires.Local().Format("2006-01-02 15:04:05"), code)
		return err
	})
}

// instanceDBRefusal is the guard of withInstanceDB. DB_PATH must be set. The
// container default database is the server's own: it takes hosting rows only
// while the instance has SWITCHBOARD_HOSTING on (the same reading as the
// server start), so an instance with hosting off never gains any. Any other
// path is an instance database of its own (`up`).
func instanceDBRefusal(cmd, path string, getenv func(string) string) error {
	switch {
	case path == "":
		return errors.New(cmd + ": DB_PATH is unset; name this instance's own database")
	case filepath.ToSlash(filepath.Clean(path)) == containerDBPath:
		if on, from := config.HostingFrom(getenv); !on {
			return errors.New(cmd + ": " + from + " is off; the database " + containerDBPath + " takes hosting records only while hosting is on")
		}
	}
	return nil
}

// withInstanceDB runs fn over the hosting service of DB_PATH, which must name
// this instance's own database, so the operations database never gains
// hosting rows.
func withInstanceDB(cmd string, fn func(*hosting.Service) error) (int, error) {
	path := os.Getenv("DB_PATH")
	if err := instanceDBRefusal(cmd, path, os.Getenv); err != nil {
		return 0, err
	}
	db, err := store.Open(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if err = fn(hosting.New(db, time.Now, func(base string) bool { return identity.RoleExists(db, base) }).WithCredentials(roleCredentials(db, time.Now))); err != nil {
		return 0, err
	}
	return 0, nil
}
